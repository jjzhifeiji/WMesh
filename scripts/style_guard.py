#!/usr/bin/env python3
"""WMesh 治理门禁：拦住缺注释和分层回退。

检查两侧 server 的全部 Go（含测试）：
包注释、类型、字段、函数、常量变量，以及函数体内的语句
（调用、判断、分支、赋值）。纯 return / break / continue 由所属判断说明。
"""

from __future__ import annotations

import os
import re
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SIDES = (
    os.path.join(ROOT, "global", "server"),
    os.path.join(ROOT, "factory", "server"),
)

NAMED_FUNC = re.compile(r"^func(?:\s+\([^)]*\))?\s+[A-Za-z_][A-Za-z0-9]*")
PKG_LINE = re.compile(r"^package\s+([A-Za-z_][A-Za-z0-9]*)")
HANG_OPS = (
    "&&",
    "||",
    ":=",
    "+=",
    "-=",
    "&=",
    "|=",
    "^=",
    "<<",
    ">>",
    ",",
    "+",
    "-",
    "*",
    "/",
    "%",
    "&",
    "|",
    "^",
    "!",
    ".",
    "=",
    "<",
    ">",
)


class Mask:
    """跨行字符串和块注释的扫描状态。"""

    def __init__(self) -> None:
        self.in_str: str | None = None
        self.escape = False
        self.in_block = False


class Frame:
    """花括号帧：函数体、分支体、结构体、接口或复合字面量。"""

    def __init__(self, kind: str, paren: int, bracket: int) -> None:
        self.kind = kind
        self.paren = paren
        self.bracket = bracket


class State:
    """结构扫描状态。stmt 只用于判断紧挨着的括号属于谁。"""

    def __init__(self) -> None:
        self.frames: list[Frame] = []
        self.parens: list[str] = []
        self.bracket = 0
        self.hang = False
        self.stmt = ""


def is_doc_comment(text: str) -> bool:
    """算说明的行注释。构建指令和 nolint 不算。"""
    body = text.strip()
    if not body.startswith("//"):
        return False
    body = body[2:].strip()
    if not body:
        return False
    if body.startswith(("go:", "+build", "nolint", "export ", "line ")):
        return False
    return True


def mask_line(line: str, st: Mask) -> tuple[str, bool]:
    """去掉字符串和注释，避免把字面量里的括号当成代码。返回代码和行尾说明。"""
    out: list[str] = []
    inline = False
    i = 0
    n = len(line)
    while i < n:
        c = line[i]
        if st.in_block:
            if c == "*" and i + 1 < n and line[i + 1] == "/":
                st.in_block = False
                out.append("  ")
                i += 2
                continue
            out.append(" ")
            i += 1
            continue
        if st.in_str:
            if st.in_str == "`":
                if c == "`":
                    st.in_str = None
                out.append(" ")
                i += 1
                continue
            if st.escape:
                st.escape = False
                out.append(" ")
                i += 1
                continue
            if c == "\\":
                st.escape = True
                out.append(" ")
                i += 1
                continue
            if c == st.in_str:
                st.in_str = None
            out.append(" ")
            i += 1
            continue
        if c == "/" and i + 1 < n and line[i + 1] == "/":
            inline = is_doc_comment(line[i:])
            break
        if c == "/" and i + 1 < n and line[i + 1] == "*":
            st.in_block = True
            out.append("  ")
            i += 2
            continue
        if c in ("'", '"', "`"):
            st.in_str = c
            st.escape = False
            out.append(" ")
            i += 1
            continue
        out.append(c)
        i += 1
    return "".join(out), inline


def line_hangs(code: str) -> bool:
    """这一行的语句还没写完，下一行算续行。"""
    s = code.rstrip()
    if not s or s.endswith(("++", "--")):
        return False
    return s.endswith(HANG_OPS)


def _cut_suffix(prefix: str, extra: str, semis: bool) -> str:
    """取最近分隔符之后的片段，避免把上一句的关键字算到这一句。"""
    cut = max(prefix.rfind("{"), prefix.rfind("}"))
    if semis:
        cut = max(cut, prefix.rfind(";"))
    for ch in extra:
        cut = max(cut, prefix.rfind(ch))
    return prefix[cut + 1 :].rstrip()


def classify_brace(prefix: str) -> str:
    """判断这个左花括号是函数体、分支、结构体、接口还是复合字面量。

    不能在圆括号或分号处截断：函数参数和 for 三句式的关键字在它们左边。
    """
    p = _cut_suffix(prefix, "", False)
    if re.search(r"\bstruct$", p):
        return "struct"
    if re.search(r"\binterface$", p):
        return "iface"
    keys = list(re.finditer(r"\b(if|else|for|switch|select|func)\b", p))
    if not keys:
        return "comp"
    last = keys[-1].group(1)
    if last == "func":
        return "fn"
    return "blk"


def classify_paren(prefix: str) -> str:
    """判断左圆括号是成组声明还是普通调用。"""
    p = _cut_suffix(prefix, "()", True)
    if re.search(r"\bconst$", p):
        return "const"
    if re.search(r"\bvar$", p):
        return "var"
    if re.search(r"\bimport$", p):
        return "import"
    if re.search(r"\btype$", p):
        return "type"
    return "p"


def feed(state: State, code: str) -> None:
    """把一行代码推进括号栈。"""
    if state.stmt and not state.stmt.endswith("\n"):
        state.stmt += "\n"
    for ch in code:
        state.stmt += ch
        if ch == "{":
            kind = classify_brace(state.stmt[:-1])
            state.frames.append(Frame(kind, len(state.parens), state.bracket))
        elif ch == "}":
            if state.frames:
                state.frames.pop()
        elif ch == "(":
            state.parens.append(classify_paren(state.stmt[:-1]))
        elif ch == ")":
            if state.parens:
                state.parens.pop()
        elif ch == "[":
            state.bracket += 1
        elif ch == "]":
            if state.bracket:
                state.bracket -= 1
    state.hang = line_hangs(code)


def innermost(state: State) -> Frame | None:
    return state.frames[-1] if state.frames else None


def body_frame(state: State) -> Frame | None:
    """当前所在的函数体或分支体。复合字面量包在外面时不算。"""
    for fr in reversed(state.frames):
        if fr.kind in ("fn", "blk"):
            return fr
    return None


def in_body(state: State) -> bool:
    fr = innermost(state)
    return fr is not None and fr.kind in ("fn", "blk")


def at_group_level(state: State, kind: str) -> bool:
    """正处在 const/var/type 分组里，且没有钻进内层调用。"""
    return bool(state.parens) and state.parens[-1] == kind and not state.hang and state.bracket == 0


def logic_of(code: str) -> str:
    """去掉行首的闭括号。只剩 `,` `)` `()` 时是外层调用收口，不是新语句。"""
    s = code.strip()
    if not s.startswith("}"):
        return s
    rest = re.sub(r"^[}\s]+", "", s)
    if re.search(r"\b(else|func)\b", rest):
        return rest
    if rest == "" or rest[0] in ",)(":
        return ""
    return rest


def statement_reason(s: str) -> str | None:
    """语句缺哪一类说明。纯 return、break、continue 和标签不单列。"""
    s = s.strip()
    if not s or s == "{":
        return None
    if re.match(r"return\b", s) and "(" not in s:
        return None
    if re.match(r"(break|continue|fallthrough)\b", s):
        return None
    if re.match(r"(if|else|for|switch|select|case|default)\b", s):
        return "缺分支注释"
    if re.match(r"[A-Za-z_][A-Za-z0-9_]*:\s*$", s):
        return None
    if re.match(r"(var|const)\s*\(\s*$", s):
        return "缺逻辑注释"
    if re.match(r"(defer|go)\b", s) or "(" in s:
        return "缺调用注释"
    return "缺逻辑注释"


def same_line_member(code: str) -> bool:
    """同一行里写了结构体或接口的成员，行尾必须再有字段说明。"""
    return any(
        m.group(1).strip()
        for m in re.finditer(r"\b(?:struct|interface)\s*\{([^}]*)\}", code)
    )


def is_named_func(code: str) -> bool:
    return NAMED_FUNC.match(code.strip()) is not None


def is_type_decl(code: str) -> bool:
    return code.strip().startswith("type ") or code.strip() == "type"


def is_single_decl(code: str) -> str | None:
    """包级单行 const/var。分组头 const ( 不算。"""
    s = code.strip()
    m = re.match(r"(const|var)\s+[A-Za-z_]", s)
    if not m:
        return None
    return "缺常量注释" if m.group(1) == "const" else "缺变量注释"


def field_reason(code: str, kind: str) -> str | None:
    """结构体字段或接口方法。空行和右括号不是成员。"""
    s = code.strip()
    if not s or s.startswith("}") or s in {"{", ")", "("}:
        return None
    if kind == "iface" and "(" in s:
        return "缺作用说明"
    return "缺字段注释"


def is_spec_line(code: str) -> bool:
    s = code.strip()
    if not s or s.startswith((")", "}")):
        return False
    return True


def package_documented(lines: list[str]) -> tuple[str | None, bool]:
    """包名，以及 package 行上方是否有说明（指令行可以夹在中间）。"""
    for i, line in enumerate(lines):
        m = PKG_LINE.match(line.strip())
        if not m:
            continue
        documented = False
        j = i - 1
        while j >= 0:
            s = lines[j].strip()
            if s == "" or (s.startswith("//") and not is_doc_comment(s)):
                j -= 1
                continue
            if is_doc_comment(s):
                documented = True
                j -= 1
                continue
            break
        return m.group(1), documented
    return None, False


def scan_lines(lines: list[str]) -> list[tuple[int, str]]:
    """返回 (行号, 原因)。行号从 1 计。"""
    bad: list[tuple[int, str]] = []
    mask = Mask()
    state = State()
    armed = False
    for idx, line in enumerate(lines):
        in_literal = mask.in_str is not None or mask.in_block
        stripped = line.strip()
        if not in_literal and stripped == "":
            continue
        if not in_literal and stripped.startswith("//"):
            if is_doc_comment(stripped):
                armed = True
            continue
        if not in_literal and stripped.startswith("/*") and "*/" in stripped and stripped.endswith("*/"):
            if stripped[2:-2].strip():
                armed = True
            continue
        code, inline = mask_line(line, mask)
        if in_literal and not code.strip():
            feed(state, code)
            continue
        covered = armed or inline
        armed = False
        reasons = line_reasons(code, covered, inline, state)
        if reasons and code.strip():
            snippet = stripped[:80]
            for reason in reasons:
                bad.append((idx + 1, f"{reason}: {snippet}"))
        feed(state, code)
    return bad


def line_reasons(code: str, covered: bool, inline: bool, state: State) -> list[str]:
    """按进入这一行之前的结构，判断这一行缺什么说明。"""
    reasons: list[str] = []
    fr = innermost(state)
    body = in_body(state)
    base = body_frame(state)
    cont = state.hang or (
        base is not None and (len(state.parens) > base.paren or state.bracket > base.bracket)
    )
    package_level = not body and len(state.parens) == 0 and not state.hang and (fr is None or fr.kind not in ("struct", "iface"))

    if package_level and is_named_func(code):
        if not covered:
            reasons.append("缺作用说明")
        if same_line_member(code) and not inline:
            reasons.append("缺字段注释")
        return reasons

    if is_type_decl(code) and not body and not cont and (fr is None or fr.kind not in ("struct", "iface", "fn", "blk")):
        if not covered:
            reasons.append("缺类型注释")
    elif at_group_level(state, "type") and is_spec_line(code) and not covered:
        reasons.append("缺类型注释")

    if same_line_member(code) and not inline and "缺字段注释" not in reasons:
        reasons.append("缺字段注释")

    if (
        fr is not None
        and fr.kind in ("struct", "iface")
        and len(state.parens) == fr.paren
        and state.bracket == fr.bracket
        and not state.hang
        and "缺字段注释" not in reasons
        and "缺作用说明" not in reasons
    ):
        why = field_reason(code, fr.kind)
        if why and not covered:
            reasons.append(why)

    if at_group_level(state, "const") and is_spec_line(code) and not covered:
        reasons.append("缺常量注释")
    elif at_group_level(state, "var") and is_spec_line(code) and not covered:
        reasons.append("缺变量注释")
    elif package_level and not reasons:
        why = is_single_decl(code)
        if why and not covered:
            reasons.append(why)

    # 回调、go/defer 里的函数字面量也要有作用说明，即使它嵌在外层调用参数里。
    if (
        fr is not None
        and fr.kind in ("fn", "blk", "comp")
        and re.search(r"\bfunc\s*\(", code)
        and not is_named_func(code)
        and not covered
        and "缺作用说明" not in reasons
    ):
        reasons.append("缺作用说明")
    if body and not cont and base is not None and len(state.parens) == base.paren and state.bracket == base.bracket:
        if reasons:
            return reasons
        why = statement_reason(logic_of(code))
        if why and not covered:
            reasons.append(why)
    return reasons


def go_files(root: str, tests: bool | None) -> list[str]:
    """tests 为 None 时测试和非测试都要。"""
    out = []
    for dirpath, _, files in os.walk(root):
        if "/vendor/" in dirpath.replace("\\", "/"):
            continue
        for name in files:
            if not name.endswith(".go"):
                continue
            is_test = name.endswith("_test.go")
            if tests is not None and tests != is_test:
                continue
            out.append(os.path.join(dirpath, name))
    return out


def rel(path: str) -> str:
    return os.path.relpath(path, ROOT)


def directory_package_docs(directory: str) -> dict[str, bool]:
    """同目录里每个包是否已经有包注释。子集扫描不能把包注释算丢。"""
    docs: dict[str, bool] = {}
    if not os.path.isdir(directory):
        return docs
    for name in os.listdir(directory):
        if not name.endswith(".go"):
            continue
        with open(os.path.join(directory, name), encoding="utf-8") as f:
            lines = f.read().splitlines()
        pkg, ok = package_documented(lines)
        if pkg:
            docs[pkg] = docs.get(pkg, False) or ok
    return docs


def comment_offenders(paths: list[str]) -> list[str]:
    """缺注释的位置。同一目录同一包至少一处包注释。"""
    bad: list[str] = []
    seen_dirs: set[str] = set()
    for path in paths:
        with open(path, encoding="utf-8") as f:
            lines = f.read().splitlines()
        seen_dirs.add(os.path.dirname(path))
        for lineno, reason in scan_lines(lines):
            bad.append(f"{rel(path)}:{lineno} {reason}")
    for directory in sorted(seen_dirs):
        for pkg, ok in sorted(directory_package_docs(directory).items()):
            if not ok:
                bad.append(f"{rel(directory)} package {pkg} 缺包注释")
    return bad


def layer_offenders(paths: list[str]) -> list[str]:
    """分层回退：跨库 import、AutoMigrate、Service 直连 GORM、HTTP 直连 Store。"""
    bad = []
    for path in paths:
        if path.endswith("_test.go"):
            continue
        p = path.replace("\\", "/")
        text = open(path, encoding="utf-8").read()
        if "/global/server/" in p and "wmesh/factory" in text:
            bad.append(f"{rel(path)} WAN 禁止 import wmesh/factory")
        if "/factory/server/" in p and "wmesh/global" in text:
            bad.append(f"{rel(path)} 厂内禁止 import wmesh/global")
        if "AutoMigrate" in text:
            bad.append(f"{rel(path)} 禁止 GORM AutoMigrate")
        if "/internal/service/" in p and "gorm.io" in text:
            bad.append(f"{rel(path)} Service 禁止直连 GORM")
        if "/internal/httpapi/" in p and re.search(r'"wmesh/(global|factory)/internal/store"', text):
            bad.append(f"{rel(path)} HTTP 禁止绕过 Service 直连 Store")
    return bad


def collect_paths(targets: list[str]) -> list[str]:
    """不传路径就扫两侧 server；传了就只扫这些文件或目录。"""
    if not targets:
        roots = list(SIDES)
    else:
        roots = []
        for t in targets:
            path = t if os.path.isabs(t) else os.path.join(ROOT, t)
            roots.append(path)
    out: list[str] = []
    for root in roots:
        if os.path.isfile(root) and root.endswith(".go"):
            out.append(root)
        elif os.path.isdir(root):
            out.extend(go_files(root, None))
    return out


def _expect(name: str, text: str, contains: list[str], absent: list[str]) -> list[str]:
    got = [f"{n} {r}" for n, r in scan_lines(text.splitlines())]
    joined = "\n".join(got)
    errs = []
    for c in contains:
        if c not in joined:
            errs.append(f"{name} 应含 {c!r}，实际:\n{joined}")
    for a in absent:
        if a in joined:
            errs.append(f"{name} 不应含 {a!r}，实际:\n{joined}")
    return errs


def self_test() -> int:
    """用小样例卡住扫描器，避免门禁自己认错行。"""
    errs: list[str] = []
    errs += _expect(
        "字段和纯返回",
        """
// 示例包，不管库。
package p

// 人员。
type Person struct {
	Name string // 显示名
	Age  int
}

// 表名。
func (p Person) TableName() string {
	return "persons"
}
""",
        ["缺字段注释"],
        ["缺作用说明", "Name string"],
    )
    errs += _expect(
        "分支调用与裸返回",
        """
// 示例包，不管库。
package p

// 停用。
func Disable(off bool) error {
	// 已经停用则不重复写。
	if off {
		return nil
	}
	s := true
	return fmt.Errorf("x")
}
""",
        ["缺逻辑注释", "缺调用注释"],
        ["return nil"],
    )
    errs += _expect(
        "else 与回调",
        """
// 示例包，不管库。
package p

// 听。
func Listen() error {
	// 注册并开始听。
	err := Open(Hooks{
		// 按编号核对。
		Auth: func(id int) error {
			// 空号拒绝。
			if id == 0 {
				return err
			}
			// 核对证明。
			return Check(id)
		},
	})
	if err != nil {
		return err
	} else {
		return nil
	}
}
""",
        ["缺分支注释: if err != nil", "缺分支注释: } else"],
        ["Auth:", "return Check", "if id == 0"],
    )
    errs += _expect(
        "字面量里的括号",
        """
// 示例包，不管库。
package p

// 记下原文。
func Note() {
	// 原文里的括号不是调用。
	s := `
(
if false { x() }
)
`
}
""",
        [],
        ["缺"],
    )
    errs += _expect(
        "指令不吞说明",
        """
// 示例包，不管库。
package p

// 嵌入 SQL。
//go:embed *.sql
var FS int
""",
        [],
        ["缺变量注释"],
    )
    errs += _expect(
        "只有指令不够",
        """
// 示例包，不管库。
package p

//go:embed *.sql
var FS int
""",
        ["缺变量注释"],
        [],
    )
    errs += _expect(
        "类型分组",
        """
// 示例包，不管库。
package p

// 与库同源的别名。
type (
	Person = Store
	Session = Store // 会话
)
""",
        ["缺类型注释: Person"],
        ["Session"],
    )
    errs += _expect(
        "一行结构体",
        """
// 示例包，不管库。
package p

// 更新入口。
type Updates struct{ *kernel }
""",
        ["缺字段注释"],
        ["缺类型注释"],
    )
    errs += _expect(
        "参数里的回调要有作用说明",
        """
// 示例包，不管库。
package p

// 登记。
func Boot() error {
	// 把回调交给注册表。
	err := Register(
		func() error {
			return nil
		}, out)
	return err
}
""",
        ["缺作用说明: func() error"],
        ["}, out", "return nil", "return err"],
    )
    errs += _expect(
        "函数字面量收口",
        """
// 示例包，不管库。
package p

// 后台听。
func Serve() {
	// 监听循环放后台，主流程去等退出。
	go func() {
		// 把监听结果交回去。
		errCh <- Listen()
	}()
	// 钩子失败则拒绝。
	_ = Open(Hooks{
		// 按编号决定拒绝还是放行。
		Auth: func(id int) error {
			// 空号直接拒绝。
			return Deny(id)
		},
	})
}
""",
        [],
        ["缺"],
    )
    errs += _expect(
        "for 三句式",
        """
// 示例包，不管库。
package p

// 数三下。
func Loop() {
	for i := 0; i < 3; i++ {
		n++
	}
}
""",
        ["缺分支注释: for i", "缺逻辑注释: n++"],
        [],
    )
    errs += _expect(
        "复合字面量的键",
        """
// 示例包，不管库。
package p

// 组装服务。
func New() *Srv {
	// 按配置听 HTTP。
	return &Srv{
		Addr: ":80",
	}
}
""",
        [],
        ["Addr"],
    )
    pkg_ok, pkg_doc = package_documented("// 只管示例。\npackage p\n".splitlines())
    if pkg_ok != "p" or not pkg_doc:
        errs.append("包注释应识别到")
    pkg_name, pkg_bad = package_documented("package p\n".splitlines())
    if pkg_name != "p" or pkg_bad:
        errs.append("缺包注释应识别到")
    if errs:
        print("style_guard 自检失败")
        print("\n".join(errs))
        return 1
    print("style_guard 自检通过")
    return 0


def main() -> int:
    args = sys.argv[1:]
    if "--self-test" in args:
        return self_test()
    show_all = "--all" in args
    targets = [a for a in args if not a.startswith("-")]
    paths = collect_paths(targets)
    offenders = comment_offenders(paths) + layer_offenders(paths)
    offenders.sort()
    if not offenders:
        print("style_guard ok")
        return 0
    top = offenders if show_all else offenders[:80]
    print(f"style_guard 失败（显示 {len(top)} 条，共 {len(offenders)} 条）")
    print("\n".join(top))
    return 1


if __name__ == "__main__":
    sys.exit(main())

// 业务哨兵清单：Known 要认全本包每个哨兵，库错和空错误一律不认。
package domain

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// 本包每个 Err 开头的包级变量都要列进 known，漏列会被当内部错误藏掉原因。
func TestKnownListsEverySentinel(t *testing.T) {
	// 解析本包错误定义源码，按名字核对哨兵清单。
	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	// 源码读不出就没法核对，直接停。
	if err != nil {
		// 把解析错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 定义过的哨兵名，按 Err 前缀收。
	declared := map[string]bool{}
	// known 清单里列出的名字。
	listed := map[string]bool{}
	// 逐个看包级声明，哨兵和清单都在 var 里。
	for _, decl := range file.Decls {
		// 取出通用声明，函数声明不含哨兵。
		gen, ok := decl.(*ast.GenDecl)
		// 不是 var 声明的跳过，import 和函数里没有哨兵。
		if !ok || gen.Tok != token.VAR {
			continue
		}
		// 逐条变量规格看名字和初值。
		for _, spec := range gen.Specs {
			// var 分组里每一条都应是值规格。
			vs, ok := spec.(*ast.ValueSpec)
			// 不是值规格说明源码形状变了，跳过不猜。
			if !ok {
				continue
			}
			// 逐个名字归类：哨兵记定义，清单记成员。
			for i, name := range vs.Names {
				// 按名字分流：哨兵、清单，别的变量不管。
				switch {
				// Err 开头的就是业务哨兵。
				case strings.HasPrefix(name.Name, "Err"):
					// 记下这个哨兵已定义。
					declared[name.Name] = true
				// known 清单的初值里逐个取出列出的名字。
				case name.Name == "known" && i < len(vs.Values):
					// 清单初值应是一个切片字面量。
					lit, ok := vs.Values[i].(*ast.CompositeLit)
					// 初值不是字面量就没法逐个核对，直接停。
					if !ok {
						// 清单写法变了，提示改测试或改回字面量。
						t.Fatalf("known is not a composite literal")
					}
					// 逐个元素取哨兵名字。
					for _, el := range lit.Elts {
						// 元素应是本包哨兵的名字。
						id, ok := el.(*ast.Ident)
						// 元素不是名字就没法核对，直接停。
						if !ok {
							// 清单里混进了表达式，提示只列哨兵名。
							t.Fatalf("known element is not an identifier")
						}
						// 记下清单里列了这个名字。
						listed[id.Name] = true
					}
				}
			}
		}
	}
	// 一个哨兵都没收到说明解析方式不对，核对就不可信。
	if len(declared) == 0 || len(listed) == 0 {
		// 解析结果为空就停，免得空对空误判通过。
		t.Fatalf("declared %d listed %d", len(declared), len(listed))
	}
	// 定义过的哨兵一个都不能漏列。
	for name := range declared {
		// 漏列的哨兵会被当内部错误，原因对外看不到。
		if !listed[name] {
			// 指出漏列的那个哨兵，好补进清单。
			t.Errorf("sentinel %s missing from known", name)
		}
	}
	// 清单里列的名字也必须都是本包哨兵。
	for name := range listed {
		// 列了不是哨兵的名字，说明清单写错了。
		if !declared[name] {
			// 指出多列的那个名字，好从清单删掉。
			t.Errorf("known lists %s which is not a sentinel", name)
		}
	}
}

// Known 只交回哨兵本身：包进去的细节丢掉，库错和空错误认不出。
func TestKnownReturnsSentinel(t *testing.T) {
	// 包了一层细节的业务错误，应认回哨兵本身。
	wrapped := fmt.Errorf("%w: wan key changed", ErrIntegrity)
	// 认回的不是哨兵本身就停，外层细节会跟着出网。
	if got := Known(wrapped); got != ErrIntegrity {
		// 断言没通过就停，认错了哨兵或带上了细节。
		t.Fatalf("wrapped: %v", got)
	}
	// 库错原文不是业务错误，应认不出。
	raw := errors.New(`ERROR: invalid byte sequence for encoding "UTF8": 0x00 (SQLSTATE 22021)`)
	// 库错被认成业务错误就停，库原文会出网。
	if got := Known(raw); got != nil {
		// 断言没通过就停，库错不该算业务错误。
		t.Fatalf("raw: %v", got)
	}
	// 空错误认不出就算对，认出来说明清单里混进了空值。
	if got := Known(nil); got != nil {
		// 断言没通过就停，空错误不该算业务错误。
		t.Fatalf("nil: %v", got)
	}
}

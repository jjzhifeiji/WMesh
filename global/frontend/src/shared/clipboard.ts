// WAN 默认走 HTTP，非安全上下文没有 navigator.clipboard；退回隐藏文本框 + execCommand。两条路都失败返回 false，由调用方提示手动复制。
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // 权限被拒等情况，继续走退回写法。
    }
  }
  return execCopy(text);
}

function execCopy(text: string) {
  const active = document.activeElement;
  // 弹窗有焦点锁，文本框挂到弹窗外会被抢走焦点、选区落空，所以挂在当前弹窗里。
  const host = active?.closest('[role="dialog"]') ?? document.body;
  const box = document.createElement("textarea");
  box.value = text;
  box.readOnly = true;
  box.tabIndex = -1;
  box.setAttribute("aria-hidden", "true");
  box.style.cssText = "position:fixed;top:0;left:0;width:1px;height:1px;padding:0;border:0;opacity:0;";
  host.appendChild(box);
  try {
    box.focus({ preventScroll: true });
    box.select();
    box.setSelectionRange(0, text.length);
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    box.remove();
    // 复制完把焦点还回去，键盘操作接着从按钮走。
    if (active instanceof HTMLElement) active.focus({ preventScroll: true });
  }
}

import { App } from "antd";

// 弹窗里有改动时先问一句再关，误点关闭或按 Esc 不至于丢掉手填内容。
export function useConfirmDiscard() {
  const { modal } = App.useApp();
  return (dirty: boolean, close: () => void) => {
    if (!dirty) {
      close();
      return;
    }
    modal.confirm({
      title: "放弃未保存的修改？",
      okText: "放弃",
      okButtonProps: { danger: true },
      cancelText: "继续编辑",
      onOk: close,
    });
  };
}

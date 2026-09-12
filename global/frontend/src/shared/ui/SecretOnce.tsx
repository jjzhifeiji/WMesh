import { Alert, Typography } from "antd";

type Props = {
  title: string;
  items: { label: string; value: string }[];
};

// 一次性秘密（激活码等）只在这一屏出现，关掉就再也拿不到；每项都能一键复制。
export function SecretOnce({ title, items }: Props) {
  return (
    <Alert
      type="warning"
      showIcon
      message={title}
      description={
        <div className="secret-list">
          {items.map((it) => (
            <div key={it.label} className="secret-item">
              <Typography.Text type="secondary">{it.label}</Typography.Text>
              <Typography.Text code copyable={{ text: it.value }}>
                {it.value}
              </Typography.Text>
            </div>
          ))}
        </div>
      }
    />
  );
}

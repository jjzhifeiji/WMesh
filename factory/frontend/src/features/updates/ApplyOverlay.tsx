import { Button, Modal, Progress, Typography } from "antd";
import { useEffect, useState } from "react";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

const KEY = "wmesh.factory.apply";
const EVT = "wmesh-apply-start";
const LIMIT_MS = 12 * 60 * 1000;

export type ApplyJob = {
  kind: string;
  version: number;
  versionName: string;
  title: string;
};

type ApplyRow = {
  phase: "idle" | "applying" | "ok" | "fail";
  kind?: string;
  version?: number;
  error?: string;
};

function readJob(): ApplyJob | null {
  try {
    const raw = sessionStorage.getItem(KEY);
    if (!raw) return null;
    return JSON.parse(raw) as ApplyJob;
  } catch {
    return null;
  }
}

// 确认后拦住整页，轮询本机更换进度，完成后刷新。
export function startApply(job: ApplyJob) {
  sessionStorage.setItem(KEY, JSON.stringify(job));
  window.dispatchEvent(new Event(EVT));
}

function failText(err?: string) {
  if (err === "health check failed") return "探活失败，已切回上一版本";
  if (err === "docker load failed") return "镜像加载失败，当前版本未更换";
  return err || "更换失败，当前版本未更换";
}

// 全屏进度：禁止其它操作，更新成功后自动刷新。
export function ApplyOverlay() {
  const [job, setJob] = useState<ApplyJob | null>(readJob);
  const [line, setLine] = useState("已确认，正在交给本机 updater");
  const [pct, setPct] = useState(20);
  const [fail, setFail] = useState("");
  const [done, setDone] = useState(false);

  useEffect(() => {
    const onStart = () => setJob(readJob());
    window.addEventListener(EVT, onStart);
    return () => window.removeEventListener(EVT, onStart);
  }, []);

  useEffect(() => {
    if (!job || fail || done) return;
    let stop = false;
    let down = false;
    const began = Date.now();
    const tick = async () => {
      if (stop) return;
      if (Date.now() - began > LIMIT_MS) {
        setFail("等候超时，请刷新后查看是否已装上");
        return;
      }
      try {
        const health = await fetch("/healthz", { cache: "no-store" });
        if (!health.ok) throw new Error("down");
        const row = await http.get<ApplyRow>(fpath("/software/apply"));
        if (row.phase === "fail") {
          setFail(failText(row.error));
          setPct(100);
          return;
        }
        if (row.phase === "ok") {
          setDone(true);
          setPct(100);
          setLine("更新完成，即将刷新");
          sessionStorage.removeItem(KEY);
          window.setTimeout(() => window.location.reload(), 800);
          return;
        }
        if (down) {
          setPct(80);
          setLine("新进程已起来，正在核对版本");
        } else {
          setPct(35);
          setLine("已确认，正在加载镜像");
        }
      } catch {
        down = true;
        setPct(60);
        setLine("服务已断开，正在更换镜像并重启");
      }
    };
    void tick();
    const id = window.setInterval(() => void tick(), 2000);
    return () => {
      stop = true;
      window.clearInterval(id);
    };
  }, [job, fail, done]);

  const close = () => {
    sessionStorage.removeItem(KEY);
    setJob(null);
    setFail("");
    setDone(false);
    setPct(20);
    setLine("已确认，正在交给本机 updater");
  };

  if (!job) return null;
  return (
    <Modal
      open
      centered
      title="正在更新"
      closable={false}
      maskClosable={false}
      keyboard={false}
      zIndex={4000}
      footer={fail ? <Button type="primary" onClick={close}>知道了</Button> : null}
    >
      <Typography.Paragraph>
        {job.title} {job.versionName}（v{job.version}）
      </Typography.Paragraph>
      <Progress percent={pct} status={fail ? "exception" : done ? "success" : "active"} />
      <Typography.Text type={fail ? "danger" : "secondary"}>{fail || line}</Typography.Text>
    </Modal>
  );
}

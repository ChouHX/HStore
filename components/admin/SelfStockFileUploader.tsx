"use client";

import { Button, Input, Message } from "@arco-design/web-react";
import { IconDelete, IconFile, IconUpload } from "@arco-design/web-react/icon";
import { useRef, useState } from "react";

const MAX_TOTAL_BYTES = 32 * 1024 * 1024;
const ALLOWED_EXT_RE = /\.(txt|csv|tsv|log|dat)$/i;

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

export interface SelfStockUploadResult {
  added: number;
  skipped: number;
  files: number;
  stockCount: number;
}

interface Props {
  goodsId: number;
  /** 上传成功后自动把商品切换为自营发货（用于「商品与上下架 → 自营设置」） */
  enableSelf?: boolean;
  onUploaded?: (res: SelfStockUploadResult) => void;
}

/**
 * 自营卡密文件上传器：
 * 直接将 .txt / .csv 文件通过 multipart/form-data 提交给后端流式解析并分批事务入库，
 * 避免大批量卡密粘贴到前端输入框造成页面卡死。
 */
export function SelfStockFileUploader({ goodsId, enableSelf = false, onUploaded }: Props) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [manualOpen, setManualOpen] = useState(false);
  const [manualText, setManualText] = useState("");
  const [lastResult, setLastResult] = useState<SelfStockUploadResult | null>(null);

  function addFiles(incoming: FileList | File[]) {
    const next = [...files];
    const existingKeys = new Set(next.map((f) => `${f.name}-${f.size}-${f.lastModified}`));
    let rejectedExt = false;

    for (const f of Array.from(incoming)) {
      if (f.name.includes(".") && !ALLOWED_EXT_RE.test(f.name)) {
        rejectedExt = true;
        continue;
      }
      const key = `${f.name}-${f.size}-${f.lastModified}`;
      if (!existingKeys.has(key)) {
        existingKeys.add(key);
        next.push(f);
      }
    }

    if (rejectedExt) {
      Message.warning("仅支持 .txt / .csv 纯文本格式的卡密文件");
    }
    const totalBytes = next.reduce((sum, f) => sum + f.size, 0);
    if (totalBytes > MAX_TOTAL_BYTES) {
      Message.warning("单次上传文件总大小不能超过 32MB，请分批上传");
      return;
    }
    setFiles(next);
  }

  function removeFile(idx: number) {
    setFiles((prev) => prev.filter((_, i) => i !== idx));
  }

  async function submitUpload() {
    if (!goodsId || goodsId <= 0) {
      Message.warning("请先选择要导入的自营商品");
      return;
    }
    const trimmedManual = manualOpen ? manualText.trim() : "";
    if (files.length === 0 && !trimmedManual) {
      Message.warning("请先选择要上传的卡密文件（.txt / .csv）");
      return;
    }

    setUploading(true);
    try {
      const fd = new FormData();
      fd.append("goodsId", String(goodsId));
      if (enableSelf) fd.append("enableSelf", "1");
      for (const f of files) {
        fd.append("files", f);
      }
      if (trimmedManual) {
        const manualBlob = new Blob([trimmedManual], { type: "text/plain;charset=utf-8" });
        fd.append("files", manualBlob, "manual-supplement.txt");
      }

      const res = await fetch("/api/admin/self/stocks/upload", {
        method: "POST",
        body: fd,
      });
      const j = await res.json();
      if (!j.ok) {
        Message.error(j.message ?? "导入失败");
        return;
      }

      const result: SelfStockUploadResult = {
        added: Number(j.added ?? 0),
        skipped: Number(j.skipped ?? 0),
        files: Number(j.files ?? files.length),
        stockCount: Number(j.stockCount ?? 0),
      };
      setLastResult(result);
      setFiles([]);
      setManualText("");
      if (inputRef.current) inputRef.current.value = "";

      Message.success(
        `导入完成：新增 ${result.added} 条${result.skipped > 0 ? `，跳过重复 ${result.skipped} 条` : ""}`,
      );
      onUploaded?.(result);
    } catch {
      Message.error("上传失败，请检查网络连接后重试");
    } finally {
      setUploading(false);
    }
  }

  const totalBytes = files.reduce((sum, f) => sum + f.size, 0);
  const canSubmit = Boolean(goodsId > 0 && (files.length > 0 || (manualOpen && manualText.trim().length > 0)));

  return (
    <div className="flex flex-col gap-3">
      <input
        ref={inputRef}
        type="file"
        accept=".txt,.csv,.tsv,.log,.dat,text/plain,text/csv"
        multiple
        className="hidden"
        onChange={(e) => {
          if (e.target.files?.length) addFiles(e.target.files);
          e.target.value = "";
        }}
      />

      <div
        role="button"
        tabIndex={0}
        onClick={() => inputRef.current?.click()}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            inputRef.current?.click();
          }
        }}
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={(e) => {
          e.preventDefault();
          setDragging(false);
        }}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          if (e.dataTransfer.files?.length) addFiles(e.dataTransfer.files);
        }}
        className={`flex cursor-pointer flex-col items-center justify-center rounded-lg border-2 border-dashed px-4 py-6 text-center transition ${
          dragging
            ? "border-[#165dff] bg-[#e8f3ff]/60"
            : "border-[#e5e6eb] bg-[#f7f8fa] hover:border-[#94bfff] hover:bg-[#f2f7ff]"
        }`}
      >
        <div className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-[#165dff] shadow-xs">
          <IconUpload style={{ fontSize: 20 }} />
        </div>
        <div className="mt-2 text-[13px] font-medium text-[#1d2129]">
          点击选择或拖拽卡密文件到此处上传
        </div>
        <div className="mt-1 text-[11px] leading-relaxed text-[#86909c]">
          支持 <span className="font-mono">.txt</span> / <span className="font-mono">.csv</span> 纯文本（UTF-8），每行一条卡密（如{" "}
          <span className="font-mono">账号----密码</span>），由后端流式解析并自动去重
        </div>
      </div>

      {files.length > 0 && (
        <div className="rounded-lg border border-[#e5e6eb] bg-white">
          <div className="flex items-center justify-between border-b border-[#f2f3f5] px-3 py-1.5 text-[12px] text-[#4e5969]">
            <span>
              已选 <b>{files.length}</b> 个文件（共 {formatBytes(totalBytes)}）
            </span>
            <Button size="mini" type="text" status="danger" onClick={() => setFiles([])}>
              清空
            </Button>
          </div>
          <div className="max-h-[160px] divide-y divide-[#f2f3f5] overflow-y-auto">
            {files.map((f, idx) => (
              <div key={`${f.name}-${f.size}-${f.lastModified}`} className="flex items-center gap-2 px-3 py-1.5 text-[12px]">
                <IconFile className="shrink-0 text-[#86909c]" />
                <span className="min-w-0 flex-1 truncate text-[#1d2129]" title={f.name}>
                  {f.name}
                </span>
                <span className="shrink-0 font-mono text-[11px] text-[#86909c]">{formatBytes(f.size)}</span>
                <Button
                  size="mini"
                  type="text"
                  status="danger"
                  icon={<IconDelete />}
                  aria-label={`移除 ${f.name}`}
                  onClick={() => removeFile(idx)}
                />
              </div>
            ))}
          </div>
        </div>
      )}

      <div>
        <button
          type="button"
          className="text-[12px] text-[#165dff] hover:underline"
          onClick={() => setManualOpen((o) => !o)}
        >
          {manualOpen ? "收起手动补录" : "需要临时补录几条？展开手动录入"}
        </button>
        {manualOpen && (
          <div className="mt-2">
            <Input.TextArea
              value={manualText}
              onChange={setManualText}
              placeholder={"仅用于少量临时补货（大批量请直接上方传文件），每行一条：\n账号----密码"}
              autoSize={{ minRows: 3, maxRows: 6 }}
              style={{ fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace", fontSize: 12 }}
            />
          </div>
        )}
      </div>

      {lastResult && (
        <div className="rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px] text-[#00b42a]">
          上次导入：新增 <b>{lastResult.added}</b> 条
          {lastResult.skipped > 0 ? `，跳过重复 ${lastResult.skipped} 条` : ""}，当前可用库存{" "}
          <b>{lastResult.stockCount}</b> 条。
        </div>
      )}

      <div className="flex items-center justify-end gap-2">
        <Button type="primary" loading={uploading} disabled={!canSubmit} onClick={submitUpload}>
          {uploading
            ? "正在上传并入库…"
            : files.length > 0
              ? `上传并导入（${files.length} 个文件）`
              : "提交导入"}
        </Button>
      </div>
    </div>
  );
}


"use client";

import { Button, Empty, Message, Space, Spin, Tag } from "@arco-design/web-react";
import { IconArrowDown, IconArrowUp, IconRefresh, IconSwap } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useState } from "react";

interface Board {
  id: number;
  name: string;
  total: number;
}

/**
 * 商城板块排序：调整首页板块卡片的展示顺序，保存后全站生效。
 * 顺序存在后端 sync_meta（store_board_order），前台 /api/store/home 直接按此顺序返回。
 */
export function BoardOrderSection() {
  const [boards, setBoards] = useState<Board[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const j = await fetch("/api/admin/boards/order").then((r) => r.json());
      if (j.ok) {
        setBoards((j.boards as Board[]) ?? []);
        setDirty(false);
      } else Message.error(j.message ?? "加载失败");
    } catch {
      Message.error("加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  function move(index: number, delta: number) {
    const target = index + delta;
    if (target < 0 || target >= boards.length) return;
    const next = boards.slice();
    [next[index], next[target]] = [next[target], next[index]];
    setBoards(next);
    setDirty(true);
  }

  async function save() {
    setSaving(true);
    try {
      const j = await fetch("/api/admin/boards/order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ids: boards.map((b) => b.id) }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("板块顺序已保存，全站生效");
        setDirty(false);
      } else Message.error(j.message ?? "保存失败");
    } catch {
      Message.error("保存失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#4e5969]">
        这里的顺序就是商城首页板块卡片的展示顺序，保存后对所有访客立即生效。没有商品的板块不会展示。
      </div>

      <Space>
        <Button size="small" icon={<IconRefresh />} loading={loading} onClick={load}>
          重新载入
        </Button>
        <Button size="small" type="primary" loading={saving} disabled={!dirty} onClick={save}>
          保存顺序
        </Button>
        {dirty && <span className="text-[12px] text-[#ff7d00]">有未保存的调整</span>}
      </Space>

      <Spin loading={loading} style={{ width: "100%" }}>
        {boards.length === 0 ? (
          <Empty description="暂无可排序的板块（还没有已同步的商品）" />
        ) : (
          <div className="flex flex-col gap-1">
            {boards.map((b, i) => (
              <div
                key={b.id}
                className="flex items-center gap-2 rounded border border-[#e5e6eb] bg-white px-2 py-1.5"
              >
                <span className="w-7 shrink-0 text-center font-mono text-[12px] text-[#86909c]">{i + 1}</span>
                <span className="min-w-0 flex-1 truncate text-[13px]" title={b.name}>
                  {b.name}
                </span>
                <Tag size="small">#{b.id}</Tag>
                <span className="w-20 shrink-0 text-right text-[11px] text-[#86909c]">{b.total} 个商品</span>
                <Button
                  size="mini"
                  icon={<IconArrowUp />}
                  disabled={i === 0}
                  onClick={() => move(i, -1)}
                  title="上移"
                />
                <Button
                  size="mini"
                  icon={<IconArrowDown />}
                  disabled={i === boards.length - 1}
                  onClick={() => move(i, 1)}
                  title="下移"
                />
              </div>
            ))}
          </div>
        )}
      </Spin>

      <div className="flex items-center gap-1 text-[11px] text-[#86909c]">
        <IconSwap />
        提示：上移 / 下移即可调整；未参与排序的新板块默认排在最后，按商品数降序。
      </div>
    </div>
  );
}

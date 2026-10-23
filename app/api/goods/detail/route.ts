import { NextResponse } from "next/server";
import { getDetail, isConfigured } from "@/lib/tianqi";
import { getDb } from "@/lib/db";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const id = Number(new URL(req.url).searchParams.get("id"));
  if (!id) return NextResponse.json({ ok: false, message: "缺少商品编号" }, { status: 400 });
  if (!isConfigured()) {
    return NextResponse.json({ ok: false, message: "未配置 App-Id" }, { status: 400 });
  }
  try {
    const detail = await getDetail(id);
    // 顺带刷新本地库存
    try {
      getDb()
        .prepare("UPDATE products SET stock_count = ?, stock_synced_at = ? WHERE goods_id = ?")
        .run(typeof detail.stockCount === "number" ? detail.stockCount : -1, Date.now(), id);
    } catch {
      /* ignore */
    }
    return NextResponse.json({ ok: true, detail });
  } catch (err) {
    const message = err instanceof Error ? err.message : "获取详情失败";
    return NextResponse.json({ ok: false, message }, { status: 500 });
  }
}

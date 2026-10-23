import { NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { refreshStockForProducts } from "@/lib/catalog";

export const dynamic = "force-dynamic";

/** 商城加载商品后，后台同步上游库存并返回最新库存 */
export async function GET(req: Request) {
  const sp = new URL(req.url).searchParams;
  const ids = (sp.get("ids") ?? "")
    .split(",")
    .map((v) => Number(v))
    .filter((v) => Number.isFinite(v) && v > 0)
    .slice(0, 20);
  if (ids.length === 0) return NextResponse.json({ ok: false, message: "缺少商品编号" }, { status: 400 });

  const db = getDb();
  const stmt = db.prepare("SELECT stock_count FROM products WHERE goods_id = ?");
  const items = ids.map((goodsId) => ({
    goodsId,
    stockCount: (stmt.get(goodsId) as { stock_count: number } | undefined)?.stock_count ?? -1,
  }));

  await refreshStockForProducts(items, 20).catch(() => {});

  return NextResponse.json({
    ok: true,
    stocks: Object.fromEntries(items.map((i) => [i.goodsId, i.stockCount])),
  });
}

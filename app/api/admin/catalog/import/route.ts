import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { enrichImportedStock, getDirRootName, importProducts, platformOfDir } from "@/lib/catalog";
import type { RemoteProduct } from "@/lib/types";
import { writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: { dirId?: number; items?: RemoteProduct[] };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const dirId = Number(body.dirId);
  const items = body.items ?? [];
  if (!dirId) return NextResponse.json({ ok: false, message: "缺少分类" }, { status: 400 });
  if (items.length === 0) return NextResponse.json({ ok: false, message: "未选择商品" }, { status: 400 });

  const count = importProducts(items, dirId, platformOfDir(dirId));
  await enrichImportedStock(items.map((i) => i.goodsId));
  writeAudit({
    operatorId: admin.uid,
    action: "catalog_import",
    targetType: "category",
    targetId: String(dirId),
    after: { count },
    remark: `从「${getDirRootName(dirId)}」拉取上架 ${count} 个商品`,
  });
  return NextResponse.json({ ok: true, count });
}

import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { getList, TianqiError } from "@/lib/tianqi";
import { importedGoodsSet } from "@/lib/catalog";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const sp = new URL(req.url).searchParams;
  const dirId = Number(sp.get("dirId"));
  const page = Math.max(1, Number(sp.get("page")) || 1);
  if (!dirId) return NextResponse.json({ ok: false, message: "请选择分类" }, { status: 400 });

  try {
    const data = await getList({ page, dirId: String(dirId), showDirId: "1" });
    const items = data?.items ?? [];
    const imported = importedGoodsSet(items.map((i) => i.goodsId));
    return NextResponse.json({
      ok: true,
      items: items.map((i) => ({ ...i, imported: imported.has(i.goodsId) })),
      page,
      allPage: data?.allPage || 1,
      allCount: data?.allCount || items.length,
    });
  } catch (err) {
    // 父级分类没有直属商品时上游返回"无商品数据"
    if (err instanceof TianqiError && /无商品数据|无数据/.test(err.message)) {
      return NextResponse.json({ ok: true, items: [], page, allPage: 1, allCount: 0 });
    }
    const message = err instanceof Error ? err.message : "拉取失败";
    return NextResponse.json({ ok: false, message }, { status: 500 });
  }
}

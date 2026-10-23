import { NextResponse } from "next/server";
import { listStoreProducts, type StoreSort } from "@/lib/queries";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const sp = new URL(req.url).searchParams;
  const rootIdRaw = sp.get("rootId");
  const rootId = rootIdRaw ? Number(rootIdRaw) : undefined;
  const keywordRaw = sp.get("keyword") ?? undefined;
  if (!rootId && !keywordRaw) {
    return NextResponse.json({ ok: false, message: "缺少板块或搜索词" }, { status: 400 });
  }

  const page = Math.max(1, Number(sp.get("page")) || 1);
  const pageSize = Math.min(60, Math.max(1, Number(sp.get("pageSize")) || 20));
  const sort = (sp.get("sort") as StoreSort) || "name-asc";
  const dirId = sp.get("dirId") ? Number(sp.get("dirId")) : undefined;
  const keyword = keywordRaw;

  const { items, total } = listStoreProducts({ rootId, dirId, keyword, sort, page, pageSize });
  return NextResponse.json({
    ok: true,
    items,
    total,
    page,
    pageSize,
    totalPages: Math.max(1, Math.ceil(total / pageSize)),
  });
}

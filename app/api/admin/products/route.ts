import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { countProductsAdmin, deleteProducts, listProductsAdmin, setProductManualPrice, setProductsEnabled } from "@/lib/queries";
import { recomputeAllPrices } from "@/lib/pricing";
import { writeAudit } from "@/lib/users";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const sp = new URL(req.url).searchParams;
  const { page, pageSize, offset } = parsePage(sp, 15);
  const filter = {
    keyword: sp.get("keyword") ?? undefined,
    platform: sp.get("platform") ?? undefined,
    categoryId: sp.get("categoryId") ? Number(sp.get("categoryId")) : undefined,
    rootIds: sp.get("rootIds")
      ? sp.get("rootIds")!.split(",").map(Number).filter(Boolean)
      : undefined,
    subIds: sp.get("subIds")
      ? sp.get("subIds")!.split(",").map(Number).filter(Boolean)
      : undefined,
    enabledList: sp.get("enabledList")
      ? sp.get("enabledList")!.split(",").map(Number)
      : undefined,
    enabled: sp.get("enabled") ?? undefined,
  };
  const items = listProductsAdmin({ ...filter, limit: pageSize, offset });
  const total = countProductsAdmin(filter);
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    ids?: number[];
    goodsId?: number;
    enabled?: boolean;
    price?: number | null;
    locked?: boolean;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  switch (body.action) {
    case "toggle": {
      const ids = (body.ids ?? []).map(Number).filter(Boolean);
      if (ids.length === 0) return NextResponse.json({ ok: false, message: "未选择商品" }, { status: 400 });
      const enabled = body.enabled !== false;
      setProductsEnabled(ids, enabled);
      writeAudit({
        operatorId: admin.uid,
        action: enabled ? "product_on" : "product_off",
        targetType: "product",
        targetId: ids.join(","),
        after: { enabled, count: ids.length },
        remark: enabled ? "批量上架" : "批量下架",
      });
      return NextResponse.json({ ok: true, count: ids.length });
    }
    case "price": {
      const goodsId = Number(body.goodsId);
      if (!goodsId) return NextResponse.json({ ok: false, message: "缺少商品编号" }, { status: 400 });
      setProductManualPrice(goodsId, body.price ?? null, Boolean(body.locked));
      writeAudit({
        operatorId: admin.uid,
        action: "product_price",
        targetType: "product",
        targetId: String(goodsId),
        after: { price: body.price ?? null, locked: Boolean(body.locked) },
        remark: body.locked ? "手动锁价" : "解除锁价",
      });
      return NextResponse.json({ ok: true });
    }
    case "delete": {
      const ids = (body.ids ?? []).map(Number).filter(Boolean);
      if (ids.length === 0) return NextResponse.json({ ok: false, message: "未选择商品" }, { status: 400 });
      deleteProducts(ids);
      writeAudit({
        operatorId: admin.uid,
        action: "product_delete",
        targetType: "product",
        targetId: ids.join(","),
        after: { count: ids.length },
        remark: "删除本地商品",
      });
      return NextResponse.json({ ok: true, count: ids.length });
    }
    case "recompute": {
      const n = recomputeAllPrices();
      writeAudit({ operatorId: admin.uid, action: "price_recompute", after: { count: n }, remark: "重算全部售价" });
      return NextResponse.json({ ok: true, count: n });
    }
    default:
      return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
  }
}

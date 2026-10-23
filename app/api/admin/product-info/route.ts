import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { listCustomizedProducts, setProductCustomInfo } from "@/lib/queries";
import { writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const sp = new URL(req.url).searchParams;
  const items = listCustomizedProducts(sp.get("keyword") ?? undefined);
  return NextResponse.json({ ok: true, items });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    goodsId?: number;
    name?: string;
    desc?: string;
    img?: string;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const goodsId = Number(body.goodsId);
  if (!goodsId) return NextResponse.json({ ok: false, message: "请输入商品编号" }, { status: 400 });

  if (body.action === "clear") {
    const ok = setProductCustomInfo(goodsId, null);
    if (!ok) return NextResponse.json({ ok: false, message: "商品不存在" }, { status: 404 });
    writeAudit({
      operatorId: admin.uid,
      action: "product_info_clear",
      targetType: "product",
      targetId: String(goodsId),
      remark: "清除商品自定义信息",
    });
    return NextResponse.json({ ok: true });
  }

  const name = (body.name ?? "").trim();
  const desc = (body.desc ?? "").trim();
  const img = (body.img ?? "").trim();
  const ok = setProductCustomInfo(goodsId, { name, desc, img });
  if (!ok) return NextResponse.json({ ok: false, message: "商品不存在" }, { status: 404 });
  writeAudit({
    operatorId: admin.uid,
    action: "product_info_save",
    targetType: "product",
    targetId: String(goodsId),
    after: { name, desc, img },
    remark: "设置商品自定义信息",
  });
  return NextResponse.json({ ok: true });
}

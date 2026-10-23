import { NextResponse } from "next/server";
import { createOrder, getDetail } from "@/lib/tianqi";
import { getProduct } from "@/lib/queries";
import { getOrder, markSettled, upsertCards, upsertOrder } from "@/lib/orders";
import { getSessionUser } from "@/lib/session";
import { consume, getLevelDiscount, refundConsume, settleConsumption } from "@/lib/users";

export const dynamic = "force-dynamic";

function genOuterNumber(): string {
  const rand = Math.floor(Math.random() * 9000 + 1000);
  return `FAKA${Date.now()}${rand}`;
}

export async function POST(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });

  let body: { goodsId?: number; count?: number; attach?: { name: string; value: string }[]; source?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const goodsId = Number(body.goodsId);
  const count = Number(body.count) || 1;
  const source: "admin" | "user" = user.role === "admin" && body.source === "admin" ? "admin" : "user";
  if (!goodsId) return NextResponse.json({ ok: false, message: "缺少商品编号" }, { status: 400 });
  if (count < 1) return NextResponse.json({ ok: false, message: "数量不合法" }, { status: 400 });

  const product = getProduct(goodsId);
  if (!product) return NextResponse.json({ ok: false, message: "商品不存在" }, { status: 400 });
  if (source === "user" && product.enabled !== 1) {
    return NextResponse.json({ ok: false, message: "商品已下架" }, { status: 400 });
  }
  if (source === "user" && user.status !== "active") {
    return NextResponse.json({ ok: false, message: "账号已被冻结" }, { status: 400 });
  }

  let detail;
  try {
    detail = await getDetail(goodsId);
  } catch (err) {
    return NextResponse.json(
      { ok: false, message: err instanceof Error ? err.message : "获取商品详情失败" },
      { status: 500 },
    );
  }
  if (detail.status !== 1) {
    return NextResponse.json({ ok: false, message: "商品当前不可售" }, { status: 400 });
  }
  if (count < (detail.minQuantity || 1) || count > (detail.maxQuantity || 999999)) {
    return NextResponse.json(
      { ok: false, message: `购买数量需在 ${detail.minQuantity} ~ ${detail.maxQuantity} 之间` },
      { status: 400 },
    );
  }
  if (detail.stockCount > 0 && count > detail.stockCount) {
    return NextResponse.json({ ok: false, message: "库存不足" }, { status: 400 });
  }

  // 销售价 = 定价规则价 × 会员折扣
  const discount = source === "user" ? getLevelDiscount(user.levelId) : 1;
  const unitPrice = Number((product.finalPrice * discount).toFixed(4));
  const saleMoney = Number((unitPrice * count).toFixed(4));
  const costMoney = Number(((detail.salesPrice ?? product.costPrice) * count).toFixed(4));

  // 用户下单先扣余额（下单失败则回滚）
  let deducted = false;
  if (source === "user") {
    const r = consume(user.uid, saleMoney, "");
    if (!r.ok) return NextResponse.json({ ok: false, message: r.message }, { status: 400 });
    deducted = true;
  }

  const outerNumber = genOuterNumber();
  try {
    const result = await createOrder({
      goodsId,
      count,
      outerNumber,
      safePrice: costMoney,
      sku: "",
      attach: body.attach ?? [],
    });

    upsertOrder({
      orderNumber: result.orderNumber,
      outerNumber,
      goodsId,
      goodsName: detail.name,
      count,
      money: saleMoney,
      status: result.cards?.length ? 3 : 1,
      result: "下单成功",
      platform: product.platform,
      userUid: user.uid,
      saleMoney,
      costMoney,
      source,
    });
    if (result.cards?.length) upsertCards(result.orderNumber, result.cards);

    // 结算：订单完成即累加累计消费并触发自动升级
    if (source === "user" && result.cards?.length) {
      settleConsumption(user.uid, saleMoney, result.orderNumber);
      markSettled(result.orderNumber);
    }

    return NextResponse.json({ ok: true, order: getOrder(result.orderNumber) });
  } catch (err) {
    // 上游下单失败：退回已扣余额
    if (deducted) refundConsume(user.uid, saleMoney, "", "上游下单失败退回");
    const message = err instanceof Error ? err.message : "下单失败";
    return NextResponse.json({ ok: false, message }, { status: 500 });
  }
}

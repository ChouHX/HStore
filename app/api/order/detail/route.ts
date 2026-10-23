import { NextResponse } from "next/server";
import { getOrderDetail } from "@/lib/tianqi";
import { applyRemoteOrder, getOrder, markSettled } from "@/lib/orders";
import { getSessionUser } from "@/lib/session";
import { settleConsumption } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });

  let body: { orderNumber?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const orderNumber = body.orderNumber;
  if (!orderNumber) {
    return NextResponse.json({ ok: false, message: "缺少订单号" }, { status: 400 });
  }
  const owned = getOrder(orderNumber);
  if (!owned) return NextResponse.json({ ok: false, message: "订单不存在" }, { status: 404 });
  if (user.role !== "admin" && owned.userUid !== user.uid) {
    return NextResponse.json({ ok: false, message: "无权操作" }, { status: 403 });
  }

  try {
    const detail = await getOrderDetail({ orderNumber });
    let order = applyRemoteOrder(detail);
    // 订单完成且未结算 → 累加累计消费并自动升级
    if (order.status === 3 && order.settled === 0 && order.source === "user" && order.userUid) {
      settleConsumption(order.userUid, order.saleMoney, order.orderNumber);
      markSettled(order.orderNumber);
      order = getOrder(order.orderNumber)!;
    }
    return NextResponse.json({ ok: true, order });
  } catch (err) {
    const message = err instanceof Error ? err.message : "刷新失败";
    return NextResponse.json({ ok: false, message, order: getOrder(orderNumber) }, { status: 500 });
  }
}

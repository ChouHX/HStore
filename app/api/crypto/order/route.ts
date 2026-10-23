import { NextResponse } from "next/server";
import { cancelCryptoOrder, checkCryptoOrder, createCryptoOrder, getCryptoOrder } from "@/lib/crypto";
import { getSessionUser } from "@/lib/session";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });
  let body: { cny?: number; chain?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  try {
    const order = await createCryptoOrder(user.uid, Number(body.cny), body.chain);
    return NextResponse.json({ ok: true, order });
  } catch (err) {
    return NextResponse.json(
      { ok: false, message: err instanceof Error ? err.message : "创建订单失败" },
      { status: 400 },
    );
  }
}

export async function GET(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });
  const id = Number(new URL(req.url).searchParams.get("id"));
  if (!id) return NextResponse.json({ ok: false, message: "缺少订单号" }, { status: 400 });
  const owned = getCryptoOrder(id);
  if (!owned) return NextResponse.json({ ok: false, message: "订单不存在" }, { status: 404 });
  if (owned.uid !== user.uid && user.role !== "admin") {
    return NextResponse.json({ ok: false, message: "无权查看" }, { status: 403 });
  }
  const order = await checkCryptoOrder(id);
  return NextResponse.json({ ok: true, order });
}

export async function DELETE(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });
  const id = Number(new URL(req.url).searchParams.get("id"));
  if (!id) return NextResponse.json({ ok: false, message: "缺少订单号" }, { status: 400 });
  try {
    const order = cancelCryptoOrder(id, user.role === "admin" ? undefined : user.uid);
    if (!order) return NextResponse.json({ ok: false, message: "订单不存在" }, { status: 404 });
    return NextResponse.json({ ok: true, order });
  } catch (err) {
    return NextResponse.json(
      { ok: false, message: err instanceof Error ? err.message : "取消失败" },
      { status: 400 },
    );
  }
}

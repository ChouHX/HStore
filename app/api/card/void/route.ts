import { NextResponse } from "next/server";
import { voidCard } from "@/lib/tianqi";
import { cardOwner, updateCardStatus, getOrder } from "@/lib/orders";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  let body: { cardId?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const cardId = body.cardId;
  if (!cardId) return NextResponse.json({ ok: false, message: "缺少卡券ID" }, { status: 400 });

  const owner = cardOwner(cardId);
  try {
    const r = await voidCard(cardId);
    updateCardStatus(cardId, r.status ?? 2, r.result ?? "作废成功");
    return NextResponse.json({ ok: true, status: r.status, order: owner ? getOrder(owner) : null });
  } catch (err) {
    const message = err instanceof Error ? err.message : "作废失败";
    return NextResponse.json({ ok: false, message }, { status: 500 });
  }
}

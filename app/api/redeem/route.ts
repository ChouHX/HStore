import { NextResponse } from "next/server";
import { redeem } from "@/lib/redeem";
import { getSessionUser } from "@/lib/session";
import { getUser } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });

  let body: { code?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const result = redeem(user.uid, body.code ?? "");
  const fresh = getUser(user.uid);
  return NextResponse.json({ ...result, balance: fresh?.balance ?? result.balance, user: fresh });
}

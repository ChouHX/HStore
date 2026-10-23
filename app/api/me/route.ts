import { NextResponse } from "next/server";
import { getSessionUser } from "@/lib/session";
import { getUser, listLevels } from "@/lib/users";
import { getDb } from "@/lib/db";

export const dynamic = "force-dynamic";

export async function GET() {
  const session = await getSessionUser();
  if (!session) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });
  const user = getUser(session.uid);
  if (!user) return NextResponse.json({ ok: false, message: "用户不存在" }, { status: 404 });
  const levels = listLevels();
  const orderCount = (
    getDb().prepare("SELECT COUNT(*) c FROM orders WHERE user_uid = ?").get(user.uid) as { c: number }
  ).c;
  return NextResponse.json({ ok: true, user, levels, orderCount });
}

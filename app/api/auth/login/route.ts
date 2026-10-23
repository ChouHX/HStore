import { NextResponse } from "next/server";
import { authenticate, SESSION_COOKIE, sessionToken } from "@/lib/session";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  let body: { username?: string; password?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const username = (body.username ?? "").trim();
  const password = body.password ?? "";
  if (!username || !password) {
    return NextResponse.json({ ok: false, message: "请输入用户名和密码" }, { status: 400 });
  }
  const user = authenticate(username, password);
  if (!user) {
    return NextResponse.json({ ok: false, message: "用户名或密码错误" }, { status: 401 });
  }
  if (user.status !== "active") {
    return NextResponse.json({ ok: false, message: "账号已被冻结" }, { status: 403 });
  }
  const res = NextResponse.json({ ok: true, user });
  res.cookies.set(SESSION_COOKIE, sessionToken(user.uid), {
    httpOnly: true,
    sameSite: "lax",
    path: "/",
    maxAge: 60 * 60 * 24 * 7,
  });
  return res;
}

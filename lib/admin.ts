import { NextResponse } from "next/server";
import { getSessionUser } from "./session";
import type { User } from "./types";

export async function guardAdmin(): Promise<{ user: User; res?: never } | { user?: never; res: NextResponse }> {
  const user = await getSessionUser();
  if (!user) {
    return { res: NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 }) };
  }
  if (user.role !== "admin") {
    return { res: NextResponse.json({ ok: false, message: "需要管理员权限" }, { status: 403 }) };
  }
  return { user };
}

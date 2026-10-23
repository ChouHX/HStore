import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { getAccount, isConfigured } from "@/lib/tianqi";

export const dynamic = "force-dynamic";

export async function POST() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  if (!isConfigured()) {
    return NextResponse.json({ ok: false, message: "请先填写客户编号与 API 密钥" }, { status: 400 });
  }
  try {
    const data = await getAccount();
    return NextResponse.json({ ok: true, balance: data.balance });
  } catch (err) {
    return NextResponse.json(
      { ok: false, message: err instanceof Error ? err.message : "连接失败" },
      { status: 500 },
    );
  }
}

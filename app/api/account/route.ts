import { NextResponse } from "next/server";
import { getAccount, isConfigured } from "@/lib/tianqi";

export const dynamic = "force-dynamic";

export async function GET() {
  if (!isConfigured()) {
    return NextResponse.json({ ok: false, balance: null, message: "未配置 App-Id" });
  }
  try {
    const data = await getAccount();
    return NextResponse.json({ ok: true, balance: data.balance });
  } catch (err) {
    const message = err instanceof Error ? err.message : "查询失败";
    return NextResponse.json({ ok: false, balance: null, message }, { status: 500 });
  }
}

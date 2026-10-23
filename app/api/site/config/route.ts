import { NextResponse } from "next/server";
import { getRedeemPurchaseUrl } from "@/lib/settings";

export const dynamic = "force-dynamic";

export async function GET() {
  return NextResponse.json({ ok: true, redeemPurchaseUrl: getRedeemPurchaseUrl() });
}

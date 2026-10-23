import { NextResponse } from "next/server";
import { CRYPTO_CHAINS, getCryptoConfig } from "@/lib/crypto";
import { isBybitConfigured } from "@/lib/bybit";

export const dynamic = "force-dynamic";

export async function GET() {
  const c = getCryptoConfig();
  return NextResponse.json({
    ok: true,
    enabled: c.enabled,
    configured: isBybitConfigured(),
    rate: c.rate,
    minUsdt: c.minUsdt,
    minCny: Number((c.minUsdt * c.rate).toFixed(2)),
    defaultChain: c.chain,
    chains: Object.values(CRYPTO_CHAINS),
  });
}

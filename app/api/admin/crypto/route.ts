import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import {
  CRYPTO_CHAINS,
  addPoolUid,
  checkCryptoOrder,
  expireCryptoOrders,
  getCryptoConfig,
  listCryptoOrders,
  listPool,
  refreshPoolAddresses,
  removePoolSlot,
  saveCryptoConfig,
  setPoolSlotEnabled,
  simulateCryptoOrder,
} from "@/lib/crypto";
import { getBybitConfig, isBybitConfigured } from "@/lib/bybit";
import { writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

function mask(v: string): string {
  return v ? `${v.slice(0, 4)}****${v.slice(-4)}` : "";
}

function snapshot() {
  const c = getCryptoConfig();
  const bk = getBybitConfig();
  return {
    ok: true,
    config: c,
    configured: isBybitConfigured(),
    apiKeyMasked: mask(bk.apiKey),
    hasApiKey: Boolean(bk.apiKey),
    hasApiSecret: Boolean(bk.apiSecret),
    chains: Object.values(CRYPTO_CHAINS),
    pool: listPool(),
    orders: listCryptoOrders({ limit: 100 }),
  };
}

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  expireCryptoOrders();
  return NextResponse.json(snapshot());
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    enabled?: boolean;
    chain?: string;
    rate?: number;
    minUsdt?: number;
    apiKey?: string;
    apiSecret?: string;
    uid?: string;
    id?: number;
    orderId?: number;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  switch (body.action) {
    case "saveConfig": {
      saveCryptoConfig({
        enabled: body.enabled,
        chain: body.chain,
        rate: body.rate,
        minUsdt: body.minUsdt,
        apiKey: body.apiKey,
        apiSecret: body.apiSecret,
      });
      const refresh = await refreshPoolAddresses();
      writeAudit({
        operatorId: admin.uid,
        action: "crypto_config_save",
        after: { enabled: body.enabled, chain: body.chain, rate: body.rate, minUsdt: body.minUsdt },
        remark: "保存加密充值配置",
      });
      return NextResponse.json({ ...snapshot(), refresh });
    }
    case "refreshPool": {
      const refresh = await refreshPoolAddresses();
      return NextResponse.json({ ...snapshot(), refresh });
    }
    case "addPool": {
      if (!body.uid) return NextResponse.json({ ok: false, message: "请输入子账户 UID" }, { status: 400 });
      const added = addPoolUid(body.uid);
      const refresh = await refreshPoolAddresses();
      writeAudit({ operatorId: admin.uid, action: "crypto_pool_add", targetId: body.uid, remark: "添加地址池子账户" });
      return NextResponse.json({ ...snapshot(), added, refresh });
    }
    case "removePool": {
      if (!body.id) return NextResponse.json({ ok: false, message: "缺少 ID" }, { status: 400 });
      const removed = removePoolSlot(body.id);
      writeAudit({ operatorId: admin.uid, action: "crypto_pool_remove", targetId: String(body.id), remark: "移除地址池子账户" });
      return NextResponse.json({ ...snapshot(), removed });
    }
    case "togglePool": {
      if (!body.id) return NextResponse.json({ ok: false, message: "缺少 ID" }, { status: 400 });
      const changed = setPoolSlotEnabled(body.id, Boolean(body.enabled));
      return NextResponse.json({ ...snapshot(), changed });
    }
    case "checkOrder": {
      if (!body.orderId) return NextResponse.json({ ok: false, message: "缺少订单" }, { status: 400 });
      const order = await checkCryptoOrder(body.orderId);
      return NextResponse.json({ ...snapshot(), order });
    }
    case "checkOrders": {
      expireCryptoOrders();
      const waiting = listCryptoOrders({ limit: 50 }).filter((o) => o.status === "waiting").slice(0, 10);
      for (const o of waiting) await checkCryptoOrder(o.id);
      return NextResponse.json({ ...snapshot(), checked: waiting.length });
    }
    case "simulate": {
      if (!body.orderId) return NextResponse.json({ ok: false, message: "缺少订单" }, { status: 400 });
      const order = simulateCryptoOrder(body.orderId);
      writeAudit({
        operatorId: admin.uid,
        action: "crypto_order_simulate",
        targetId: String(body.orderId),
        remark: "模拟到账（测试）",
      });
      return NextResponse.json({ ...snapshot(), order });
    }
    default:
      return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
  }
}

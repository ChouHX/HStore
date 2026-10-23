import crypto from "node:crypto";
import { getMeta } from "./db";

const MAINNET = "https://api.bybit.com";
const TESTNET = "https://api-testnet.bybit.com";
const RECV_WINDOW = "20000";

export interface BybitConfig {
  apiKey: string;
  apiSecret: string;
  baseUrl: string;
}

export function getBybitConfig(): BybitConfig {
  const env = getMeta("bybit_env") || process.env.BYBIT_ENV || "mainnet";
  return {
    apiKey: getMeta("bybit_api_key") || process.env.BYBIT_API_KEY || "",
    apiSecret: getMeta("bybit_api_secret") || process.env.BYBIT_API_SECRET || "",
    baseUrl: env === "testnet" ? TESTNET : MAINNET,
  };
}

export function isBybitConfigured(): boolean {
  const c = getBybitConfig();
  return Boolean(c.apiKey && c.apiSecret);
}

export class BybitError extends Error {
  retCode: number;
  constructor(code: number, message: string) {
    super(message);
    this.name = "BybitError";
    this.retCode = code;
  }
}

interface BybitEnvelope<T> {
  retCode: number;
  retMsg: string;
  result?: T;
}

async function signedGet<T>(path: string, params: Record<string, string | number> = {}): Promise<T> {
  const cfg = getBybitConfig();
  if (!cfg.apiKey || !cfg.apiSecret) throw new BybitError(-1, "未配置 Bybit API Key");

  const qs = Object.keys(params)
    .sort()
    .map((k) => `${k}=${encodeURIComponent(String(params[k]))}`)
    .join("&");

  for (let attempt = 0; attempt < 3; attempt++) {
    const ts = Date.now().toString();
    const sign = crypto
      .createHmac("sha256", cfg.apiSecret)
      .update(ts + cfg.apiKey + RECV_WINDOW + qs)
      .digest("hex");

    let res: Response;
    try {
      res = await fetch(`${cfg.baseUrl}${path}${qs ? "?" + qs : ""}`, {
        headers: {
          "X-BAPI-API-KEY": cfg.apiKey,
          "X-BAPI-TIMESTAMP": ts,
          "X-BAPI-RECV-WINDOW": RECV_WINDOW,
          "X-BAPI-SIGN": sign,
        },
        cache: "no-store",
      });
    } catch (err) {
      if (attempt === 2) throw new BybitError(-1, `网络错误：${err instanceof Error ? err.message : String(err)}`);
      await new Promise((r) => setTimeout(r, 800));
      continue;
    }

    const json = (await res.json()) as BybitEnvelope<T>;
    if (json.retCode === 0) return json.result as T;
    // 10002: timestamp 过期，重试一次
    if (json.retCode === 10002 && attempt < 2) {
      await new Promise((r) => setTimeout(r, 500));
      continue;
    }
    throw new BybitError(json.retCode, json.retMsg || "Bybit 请求失败");
  }
  throw new BybitError(-1, "Bybit 请求失败");
}

export interface BybitDepositRow {
  id: string;
  coin: string;
  chain: string;
  amount: string;
  txID: string;
  status: number; // 3 = success
  toAddress: string;
  successAt?: string;
  confirmations?: string;
  depositType?: string;
}

/** 子账户充值地址（chainType 传链代码：TRX / BSC / ETH …） */
export async function getSubMemberAddress(
  subUid: string,
  coin: string,
  chainCode: string,
): Promise<{ address: string; chainType: string; chain: string } | null> {
  const r = await signedGet<{ chains?: unknown }>("/v5/asset/deposit/query-sub-member-address", {
    coin,
    chainType: chainCode,
    subMemberId: subUid,
  });
  const chains = r?.chains;
  const list = Array.isArray(chains) ? chains : chains ? [chains] : [];
  const hit = (list as { addressDeposit?: string; chainType?: string; chain?: string }[]).find(
    (c) => c?.addressDeposit,
  );
  if (!hit?.addressDeposit) return null;
  return { address: hit.addressDeposit, chainType: hit.chainType ?? chainCode, chain: hit.chain ?? chainCode };
}

/** 子账户充值记录 */
export async function getSubMemberDeposits(
  subUid: string,
  coin: string,
  startTime?: number,
): Promise<BybitDepositRow[]> {
  const params: Record<string, string | number> = { subMemberId: subUid, coin, limit: 50 };
  if (startTime) params.startTime = startTime;
  const r = await signedGet<{ rows?: BybitDepositRow[] }>(
    "/v5/asset/deposit/query-sub-member-record",
    params,
  );
  return r?.rows ?? [];
}

/** 子账户列表 */
export async function getSubMembers(): Promise<{ uid: string; username: string; status: number }[]> {
  const r = await signedGet<{ subMembers?: { uid: string; username: string; status: number }[] }>(
    "/v5/user/query-sub-members",
  );
  return r?.subMembers ?? [];
}

/** 支持充值的链（用于后台展示） */
export async function getAllowedDepositChains(
  coin: string,
): Promise<{ chain: string; chainType: string; depositSwitch: string; minDepositAmount: string; blockConfirmNumber: number }[]> {
  const r = await signedGet<{
    configList?: {
      chain: string;
      chainType: string;
      depositSwitch: string;
      minDepositAmount: string;
      blockConfirmNumber: number;
    }[];
  }>("/v5/asset/deposit/query-allowed-list", { coin });
  return r?.configList ?? [];
}

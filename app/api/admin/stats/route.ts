import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { getDb } from "@/lib/db";

export const dynamic = "force-dynamic";

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const db = getDb();

  const products = db
    .prepare(
      `SELECT COUNT(*) AS total,
              SUM(CASE WHEN enabled = 1 THEN 1 ELSE 0 END) AS enabled,
              SUM(CASE WHEN enabled = 0 THEN 1 ELSE 0 END) AS disabled
       FROM products`,
    )
    .get() as { total: number; enabled: number; disabled: number };

  const users = db
    .prepare(
      `SELECT COUNT(*) AS total,
              SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) AS active,
              SUM(CASE WHEN status = 'frozen' THEN 1 ELSE 0 END) AS frozen,
              ROUND(SUM(balance), 2) AS balance
       FROM users`,
    )
    .get() as { total: number; active: number; frozen: number; balance: number };

  const orders = db
    .prepare(
      `SELECT COUNT(*) AS total,
              ROUND(SUM(CASE WHEN source = 'user' THEN sale_money ELSE 0 END), 2) AS sale,
              ROUND(SUM(CASE WHEN source = 'user' THEN cost_money ELSE 0 END), 2) AS cost
       FROM orders`,
    )
    .get() as { total: number; sale: number; cost: number };

  const redeem = db
    .prepare(
      `SELECT (SELECT COUNT(*) FROM redeem_batches) AS batches,
              (SELECT COUNT(*) FROM redeem_codes) AS codes,
              (SELECT SUM(CASE WHEN status = 'used' THEN 1 ELSE 0 END) FROM redeem_codes) AS used`,
    )
    .get() as { batches: number; codes: number; used: number };

  const byPlatform = db
    .prepare("SELECT platform, COUNT(*) AS c FROM products GROUP BY platform")
    .all() as { platform: string; c: number }[];

  return NextResponse.json({
    ok: true,
    products,
    users,
    orders,
    redeem,
    byPlatform,
  });
}

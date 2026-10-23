import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { countLedger, listLedger } from "@/lib/users";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const sp = new URL(req.url).searchParams;
  const uid = sp.get("uid") ? Number(sp.get("uid")) : undefined;
  const { page, pageSize, offset } = parsePage(sp, 15);
  const items = listLedger(uid, { limit: pageSize, offset });
  const total = countLedger(uid);
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

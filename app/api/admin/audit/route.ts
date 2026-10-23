import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { countAudit, listAudit } from "@/lib/users";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const { page, pageSize, offset } = parsePage(new URL(req.url).searchParams, 20);
  const items = listAudit({ limit: pageSize, offset });
  const total = countAudit();
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

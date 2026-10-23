import { NextResponse } from "next/server";
import { getSessionUser } from "@/lib/session";
import { countLedger, listLedger } from "@/lib/users";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const session = await getSessionUser();
  if (!session) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });
  const { page, pageSize, offset } = parsePage(new URL(req.url).searchParams, 12);
  const items = listLedger(session.uid, { limit: pageSize, offset });
  const total = countLedger(session.uid);
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

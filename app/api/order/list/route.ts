import { NextResponse } from "next/server";
import { countOrders, listOrders } from "@/lib/orders";
import { getSessionUser } from "@/lib/session";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

const STATUS_GROUPS: Record<string, number[]> = {
  all: [],
  done: [3],
  processing: [1, 2, 7, 8],
};

export async function GET(req: Request) {
  const user = await getSessionUser();
  if (!user) return NextResponse.json({ ok: false, message: "请先登录" }, { status: 401 });

  const sp = new URL(req.url).searchParams;
  const { page, pageSize, offset } = parsePage(sp, 10);
  const filter = {
    uid: user.role === "admin" ? undefined : user.uid,
    statuses: STATUS_GROUPS[sp.get("status") ?? "all"] ?? [],
    keyword: sp.get("keyword") ?? undefined,
  };

  const items = listOrders({ ...filter, limit: pageSize, offset });
  const total = countOrders(filter);
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

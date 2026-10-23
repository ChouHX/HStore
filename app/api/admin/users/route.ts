import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import {
  adjustBalance,
  countLedger,
  countUsers,
  listLedger,
  listUsers,
  setUserLevel,
  updateUser,
  writeAudit,
} from "@/lib/users";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const sp = new URL(req.url).searchParams;
  const uid = sp.get("uid") ? Number(sp.get("uid")) : undefined;
  const { page, pageSize, offset } = parsePage(sp, 15);

  if (sp.get("ledger") === "1") {
    const items = listLedger(uid, { limit: pageSize, offset });
    const total = countLedger(uid);
    return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
  }

  const filter = {
    keyword: sp.get("keyword") ?? undefined,
    status: sp.get("status") ?? undefined,
    levelId: sp.get("levelId") ? Number(sp.get("levelId")) : undefined,
    role: sp.get("role") ?? undefined,
  };
  const items = listUsers(filter, { limit: pageSize, offset });
  const total = countUsers(filter);
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    uid?: number;
    nickname?: string;
    phone?: string;
    email?: string;
    status?: "active" | "frozen";
    amount?: number;
    levelId?: number | null;
    locked?: boolean;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }
  const uid = Number(body.uid);
  if (!uid) return NextResponse.json({ ok: false, message: "缺少用户 UID" }, { status: 400 });

  switch (body.action) {
    case "update": {
      const before = listUsers({ keyword: String(uid) }).find((u) => u.uid === uid);
      const user = updateUser(uid, {
        nickname: body.nickname,
        phone: body.phone,
        email: body.email,
        status: body.status,
      });
      writeAudit({ operatorId: admin.uid, action: "user_update", targetType: "user", targetId: String(uid), before, after: user, remark: "编辑用户资料" });
      return NextResponse.json({ ok: true, user });
    }
    case "balance": {
      const amount = Number(body.amount);
      if (!amount) return NextResponse.json({ ok: false, message: "调整金额不能为 0" }, { status: 400 });
      const r = adjustBalance(uid, amount, admin.uid);
      if (!r.ok) return NextResponse.json({ ok: false, message: r.message }, { status: 400 });
      return NextResponse.json({ ok: true, before: r.before, after: r.after });
    }
    case "level": {
      const user = setUserLevel(uid, body.levelId ?? null, Boolean(body.locked), admin.uid);
      return NextResponse.json({ ok: true, user });
    }
    default:
      return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
  }
}

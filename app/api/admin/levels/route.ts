import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { createLevel, deleteLevel, listLevels, recomputeAllLevels, updateLevel, writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  return NextResponse.json({ ok: true, levels: listLevels() });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    id?: number;
    name?: string;
    threshold?: number;
    discount?: number;
    sort?: number;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  switch (body.action) {
    case "create": {
      if (!body.name) return NextResponse.json({ ok: false, message: "请填写等级名称" }, { status: 400 });
      const level = createLevel({
        name: body.name,
        threshold: Number(body.threshold) || 0,
        discount: body.discount == null ? 1 : Number(body.discount),
        sort: body.sort,
      });
      recomputeAllLevels();
      writeAudit({ operatorId: admin.uid, action: "level_create", targetType: "level", targetId: String(level.id), after: level, remark: "新增会员等级" });
      return NextResponse.json({ ok: true, level });
    }
    case "update": {
      if (!body.id) return NextResponse.json({ ok: false, message: "缺少等级 ID" }, { status: 400 });
      const before = listLevels().find((l) => l.id === body.id);
      updateLevel(body.id, {
        name: body.name,
        threshold: body.threshold == null ? undefined : Number(body.threshold),
        discount: body.discount == null ? undefined : Number(body.discount),
        sort: body.sort,
      });
      recomputeAllLevels();
      const after = listLevels().find((l) => l.id === body.id);
      writeAudit({ operatorId: admin.uid, action: "level_update", targetType: "level", targetId: String(body.id), before, after, remark: "修改会员等级" });
      return NextResponse.json({ ok: true, level: after });
    }
    case "delete": {
      if (!body.id) return NextResponse.json({ ok: false, message: "缺少等级 ID" }, { status: 400 });
      const before = listLevels().find((l) => l.id === body.id);
      deleteLevel(body.id);
      writeAudit({ operatorId: admin.uid, action: "level_delete", targetType: "level", targetId: String(body.id), before, remark: "删除会员等级" });
      return NextResponse.json({ ok: true });
    }
    default:
      return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
  }
}

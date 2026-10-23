import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { deleteAnnouncement, listAnnouncements, saveAnnouncement, toggleAnnouncement } from "@/lib/announcements";
import { writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  return NextResponse.json({ ok: true, items: listAnnouncements() });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    id?: number;
    type?: "normal" | "popup";
    title?: string;
    content?: string;
    enabled?: boolean;
    sort?: number;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  switch (body.action) {
    case "save": {
      if (!body.title?.trim()) return NextResponse.json({ ok: false, message: "请填写公告标题" }, { status: 400 });
      const item = saveAnnouncement({
        id: body.id,
        type: body.type ?? "normal",
        title: body.title.trim(),
        content: body.content ?? "",
        enabled: body.enabled !== false,
        sort: Number(body.sort) || 0,
      });
      writeAudit({
        operatorId: admin.uid,
        action: body.id ? "announcement_update" : "announcement_create",
        targetType: "announcement",
        targetId: String(item.id),
        after: { type: item.type, title: item.title, enabled: item.enabled },
        remark: "保存公告",
      });
      return NextResponse.json({ ok: true, item, items: listAnnouncements() });
    }
    case "delete": {
      if (!body.id) return NextResponse.json({ ok: false, message: "缺少 ID" }, { status: 400 });
      deleteAnnouncement(body.id);
      writeAudit({ operatorId: admin.uid, action: "announcement_delete", targetId: String(body.id), remark: "删除公告" });
      return NextResponse.json({ ok: true, items: listAnnouncements() });
    }
    case "toggle": {
      if (!body.id) return NextResponse.json({ ok: false, message: "缺少 ID" }, { status: 400 });
      toggleAnnouncement(body.id, Boolean(body.enabled));
      return NextResponse.json({ ok: true, items: listAnnouncements() });
    }
    default:
      return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
  }
}

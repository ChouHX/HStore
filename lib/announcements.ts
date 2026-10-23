import { getDb } from "./db";

export type AnnouncementType = "normal" | "popup";

export interface Announcement {
  id: number;
  type: AnnouncementType;
  title: string;
  content: string;
  enabled: number;
  sort: number;
  createdAt: number;
  updatedAt: number;
}

interface AnnRow {
  id: number;
  type: AnnouncementType;
  title: string;
  content: string;
  enabled: number;
  sort: number;
  created_at: number;
  updated_at: number;
}

function map(r: AnnRow): Announcement {
  return {
    id: r.id,
    type: r.type,
    title: r.title,
    content: r.content,
    enabled: r.enabled,
    sort: r.sort,
    createdAt: r.created_at,
    updatedAt: r.updated_at,
  };
}

export function listAnnouncements(): Announcement[] {
  return (getDb()
    .prepare("SELECT * FROM announcements ORDER BY type ASC, sort DESC, id DESC")
    .all() as AnnRow[]).map(map);
}

export function getPublicAnnouncements(): { normal: Announcement[]; popup: Announcement | null } {
  const rows = listAnnouncements().filter((a) => a.enabled === 1);
  const normal = rows.filter((a) => a.type === "normal");
  const popup = rows.find((a) => a.type === "popup") ?? null;
  return { normal, popup };
}

export function saveAnnouncement(input: {
  id?: number;
  type: AnnouncementType;
  title: string;
  content: string;
  enabled: boolean;
  sort: number;
}): Announcement {
  const db = getDb();
  const now = Date.now();
  const result = db.transaction(() => {
    if (input.type === "popup" && input.enabled) {
      // 仅允许一个启用的弹窗
      db.prepare("UPDATE announcements SET enabled=0, updated_at=? WHERE type='popup' AND id<>?").run(
        now,
        input.id ?? 0,
      );
    }
    if (input.id) {
      db.prepare(
        "UPDATE announcements SET type=?, title=?, content=?, enabled=?, sort=?, updated_at=? WHERE id=?",
      ).run(input.type, input.title, input.content, input.enabled ? 1 : 0, input.sort, now, input.id);
      return input.id;
    }
    const info = db
      .prepare(
        "INSERT INTO announcements(type, title, content, enabled, sort, created_at, updated_at) VALUES(?,?,?,?,?,?,?)",
      )
      .run(input.type, input.title, input.content, input.enabled ? 1 : 0, input.sort, now, now);
    return Number(info.lastInsertRowid);
  })();
  return listAnnouncements().find((a) => a.id === result)!;
}

export function deleteAnnouncement(id: number): void {
  getDb().prepare("DELETE FROM announcements WHERE id=?").run(id);
}

export function toggleAnnouncement(id: number, enabled: boolean): void {
  const db = getDb();
  const now = Date.now();
  const row = db.prepare("SELECT type FROM announcements WHERE id=?").get(id) as { type: AnnouncementType } | undefined;
  if (!row) return;
  if (row.type === "popup" && enabled) {
    // 启用弹窗时关闭其他弹窗，保证仅一个生效
    db.prepare("UPDATE announcements SET enabled=0, updated_at=? WHERE type='popup' AND id<>?").run(now, id);
  }
  db.prepare("UPDATE announcements SET enabled=?, updated_at=? WHERE id=?").run(enabled ? 1 : 0, now, id);
}

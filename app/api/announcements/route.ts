import { NextResponse } from "next/server";
import { getPublicAnnouncements } from "@/lib/announcements";

export const dynamic = "force-dynamic";

export async function GET() {
  return NextResponse.json({ ok: true, ...getPublicAnnouncements() });
}

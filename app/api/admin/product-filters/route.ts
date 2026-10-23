import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { getProductFilterOptions } from "@/lib/queries";

export const dynamic = "force-dynamic";

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  return NextResponse.json({ ok: true, ...getProductFilterOptions() });
}

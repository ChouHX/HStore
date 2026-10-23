import { NextResponse } from "next/server";
import { runSync } from "@/lib/sync";
import { loadDemoData } from "@/lib/demo-data";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  const url = new URL(req.url);
  const demo = url.searchParams.get("demo") === "1";
  try {
    if (demo) {
      const r = loadDemoData();
      return NextResponse.json({
        ok: true,
        source: "demo",
        message: `已载入演示数据：${r.products} 个商品`,
      });
    }
    const result = await runSync();
    return NextResponse.json(result);
  } catch (err) {
    const message = err instanceof Error ? err.message : "同步失败";
    return NextResponse.json({ ok: false, source: "remote", message }, { status: 500 });
  }
}

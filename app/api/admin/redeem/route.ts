import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { countCodes, createCodes, exportCsv, listCodes, voidCode } from "@/lib/redeem";
import { writeAudit } from "@/lib/users";
import { makePaged, parsePage } from "@/lib/pagination";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const sp = new URL(req.url).searchParams;
  const filter = {
    status: sp.get("status") ?? undefined,
    keyword: sp.get("keyword") ?? undefined,
    usedBy: sp.get("usedBy") ? Number(sp.get("usedBy")) : undefined,
  };

  if (sp.get("export") === "1") {
    const csv = exportCsv(filter);
    return new NextResponse("\uFEFF" + csv, {
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": `attachment; filename="redeem-codes.csv"`,
      },
    });
  }

  const { page, pageSize, offset } = parsePage(sp, 15);
  const items = listCodes(filter, { limit: pageSize, offset });
  const total = countCodes(filter);
  return NextResponse.json({ ok: true, ...makePaged(items, total, page, pageSize) });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    faceValue?: number;
    count?: number;
    startAt?: number | null;
    endAt?: number | null;
    codeId?: number;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  switch (body.action) {
    case "create": {
      const faceValue = Number(body.faceValue);
      const count = Number(body.count);
      if (!(faceValue > 0)) return NextResponse.json({ ok: false, message: "面值必须大于 0" }, { status: 400 });
      if (!(count > 0)) return NextResponse.json({ ok: false, message: "生成数量必须大于 0" }, { status: 400 });
      if (body.startAt && body.endAt && body.startAt > body.endAt) {
        return NextResponse.json({ ok: false, message: "开始时间不能晚于结束时间" }, { status: 400 });
      }
      const r = createCodes({
        faceValue,
        count,
        startAt: body.startAt ?? null,
        endAt: body.endAt ?? null,
      });
      writeAudit({
        operatorId: admin.uid,
        action: "redeem_create",
        targetType: "redeem_code",
        after: { faceValue, count: r.count },
        remark: `生成兑换码 ${r.count} 张，面值 ${faceValue}`,
      });
      return NextResponse.json({ ok: true, count: r.count });
    }
    case "voidCode": {
      if (!body.codeId) return NextResponse.json({ ok: false, message: "缺少券码 ID" }, { status: 400 });
      voidCode(body.codeId);
      writeAudit({
        operatorId: admin.uid,
        action: "redeem_code_void",
        targetType: "redeem_code",
        targetId: String(body.codeId),
        remark: "单码作废",
      });
      return NextResponse.json({ ok: true });
    }
    default:
      return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
  }
}

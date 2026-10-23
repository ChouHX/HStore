import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { createRule, deleteRule, listRules, recomputeAllPrices, updateRule } from "@/lib/pricing";
import { writeAudit } from "@/lib/users";
import type { PricingFormula, Rounding } from "@/lib/types";

export const dynamic = "force-dynamic";

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  return NextResponse.json({ ok: true, rules: listRules() });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: {
    action?: string;
    id?: number;
    name?: string;
    scope?: "global" | "platform" | "category" | "product";
    scopeId?: string;
    formulaType?: PricingFormula;
    amount?: number;
    percent?: number;
    multiplier?: number;
    fixed?: number;
    rounding?: Rounding;
    priority?: number;
    enabled?: number;
  };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  try {
    switch (body.action) {
      case "create": {
        if (!body.name) return NextResponse.json({ ok: false, message: "请填写规则名称" }, { status: 400 });
        const rule = createRule({
          name: body.name,
          scope: body.scope ?? "global",
          scopeId: body.scopeId ?? "",
          formulaType: body.formulaType ?? "fixed_add",
          amount: body.amount,
          percent: body.percent,
          multiplier: body.multiplier,
          fixed: body.fixed,
          rounding: body.rounding,
          priority: body.priority,
          enabled: body.enabled,
        });
        recomputeAllPrices();
        writeAudit({ operatorId: admin.uid, action: "pricing_create", targetType: "pricing_rule", targetId: String(rule.id), after: rule, remark: "新建定价规则" });
        return NextResponse.json({ ok: true, rule });
      }
      case "update": {
        if (!body.id) return NextResponse.json({ ok: false, message: "缺少规则 ID" }, { status: 400 });
        const before = listRules().find((r) => r.id === body.id);
        const rule = updateRule(body.id, {
          name: body.name,
          scope: body.scope,
          scopeId: body.scopeId,
          formulaType: body.formulaType,
          amount: body.amount,
          percent: body.percent,
          multiplier: body.multiplier,
          fixed: body.fixed,
          rounding: body.rounding,
          priority: body.priority,
          enabled: body.enabled,
        });
        recomputeAllPrices();
        writeAudit({ operatorId: admin.uid, action: "pricing_update", targetType: "pricing_rule", targetId: String(body.id), before, after: rule, remark: "修改定价规则" });
        return NextResponse.json({ ok: true, rule });
      }
      case "delete": {
        if (!body.id) return NextResponse.json({ ok: false, message: "缺少规则 ID" }, { status: 400 });
        const before = listRules().find((r) => r.id === body.id);
        deleteRule(body.id);
        recomputeAllPrices();
        writeAudit({ operatorId: admin.uid, action: "pricing_delete", targetType: "pricing_rule", targetId: String(body.id), before, remark: "删除定价规则" });
        return NextResponse.json({ ok: true });
      }
      default:
        return NextResponse.json({ ok: false, message: "未知操作" }, { status: 400 });
    }
  } catch (err) {
    return NextResponse.json(
      { ok: false, message: err instanceof Error ? err.message : "操作失败" },
      { status: 500 },
    );
  }
}

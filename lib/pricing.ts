import { getDb } from "./db";
import type { PricingFormula, PricingRule, Rounding } from "./types";

interface RuleRow {
  id: number;
  name: string;
  scope: "global" | "category" | "product";
  scope_id: string;
  formula_type: PricingFormula;
  amount: number;
  percent: number;
  multiplier: number;
  fixed: number;
  rounding: Rounding;
  priority: number;
  enabled: number;
  created_at: number;
  updated_at: number;
}

function mapRule(r: RuleRow): PricingRule {
  return {
    id: r.id,
    name: r.name,
    scope: r.scope,
    scopeId: r.scope_id,
    formulaType: r.formula_type,
    amount: r.amount,
    percent: r.percent,
    multiplier: r.multiplier,
    fixed: r.fixed,
    rounding: r.rounding,
    priority: r.priority,
    enabled: r.enabled,
    createdAt: r.created_at,
    updatedAt: r.updated_at,
  };
}

export function applyRounding(x: number, r: Rounding): number {
  switch (r) {
    case "ceil2":
      return Math.ceil(x * 100) / 100;
    case "floor2":
      return Math.floor(x * 100) / 100;
    case "round3":
      return Math.round(x * 1000) / 1000;
    case "round4":
      return Math.round(x * 10000) / 10000;
    case "ceil4":
      return Math.ceil(x * 10000) / 10000;
    case "floor4":
      return Math.floor(x * 10000) / 10000;
    case "ceil1":
      return Math.ceil(x * 10) / 10;
    case "round0":
      return Math.round(x);
    case "round2":
    default:
      return Math.round(x * 100) / 100;
  }
}

/** 定价公式 */
export function applyFormula(cost: number, rule: Pick<PricingRule, "formulaType" | "amount" | "percent" | "multiplier" | "fixed">): number {
  switch (rule.formulaType) {
    case "fixed_add":
      return cost + rule.amount;
    case "fixed_sub":
      return cost - rule.amount;
    case "ratio":
      return cost * (1 + rule.percent / 100);
    case "mixed":
      return cost * rule.multiplier + rule.fixed;
    default:
      return cost;
  }
}

export function listRules(): PricingRule[] {
  const rows = getDb()
    .prepare("SELECT * FROM pricing_rules ORDER BY priority DESC, id ASC")
    .all() as RuleRow[];
  return rows.map(mapRule);
}

export function getRule(id: number): PricingRule | null {
  const r = getDb().prepare("SELECT * FROM pricing_rules WHERE id = ?").get(id) as RuleRow | undefined;
  return r ? mapRule(r) : null;
}

export interface RuleInput {
  name: string;
  scope: "global" | "platform" | "category" | "product";
  scopeId?: string;
  formulaType: PricingFormula;
  amount?: number;
  percent?: number;
  multiplier?: number;
  fixed?: number;
  rounding?: Rounding;
  priority?: number;
  enabled?: number;
}

export function createRule(input: RuleInput): PricingRule {
  const now = Date.now();
  const info = getDb()
    .prepare(
      `INSERT INTO pricing_rules(name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at)
       VALUES(@name, @scope, @scopeId, @formulaType, @amount, @percent, @multiplier, @fixed, @rounding, @priority, @enabled, @now, @now)`,
    )
    .run({
      name: input.name,
      scope: input.scope,
      scopeId: input.scopeId ?? "",
      formulaType: input.formulaType,
      amount: input.amount ?? 0,
      percent: input.percent ?? 0,
      multiplier: input.multiplier ?? 1,
      fixed: input.fixed ?? 0,
      rounding: input.rounding ?? "round2",
      priority: input.priority ?? 0,
      enabled: input.enabled ?? 1,
      now,
    });
  return getRule(Number(info.lastInsertRowid))!;
}

export function updateRule(id: number, input: Partial<RuleInput>): PricingRule | null {
  const cur = getRule(id);
  if (!cur) return null;
  getDb()
    .prepare(
      `UPDATE pricing_rules SET name=@name, scope=@scope, scope_id=@scopeId, formula_type=@formulaType,
        amount=@amount, percent=@percent, multiplier=@multiplier, fixed=@fixed, rounding=@rounding,
        priority=@priority, enabled=@enabled, updated_at=@now WHERE id=@id`,
    )
    .run({
      id,
      name: input.name ?? cur.name,
      scope: input.scope ?? cur.scope,
      scopeId: input.scopeId ?? cur.scopeId,
      formulaType: input.formulaType ?? cur.formulaType,
      amount: input.amount ?? cur.amount,
      percent: input.percent ?? cur.percent,
      multiplier: input.multiplier ?? cur.multiplier,
      fixed: input.fixed ?? cur.fixed,
      rounding: input.rounding ?? cur.rounding,
      priority: input.priority ?? cur.priority,
      enabled: input.enabled ?? cur.enabled,
      now: Date.now(),
    });
  return getRule(id);
}

export function deleteRule(id: number): void {
  getDb().prepare("DELETE FROM pricing_rules WHERE id = ?").run(id);
}

const SPECIFICITY: Record<PricingRule["scope"], number> = { global: 0, platform: 1, category: 2, product: 3 };

/** 为单个商品解析价格：取最具体的匹配规则；无规则则返回成本价 */
export function resolvePrice(
  cost: number,
  ctx: { goodsId: number; dirIds: number[]; platform: string },
): { price: number; ruleId: number | null } {
  const rules = listRules().filter((r) => r.enabled === 1);
  const matched = rules.filter((r) => {
    if (r.scope === "global") return true;
    if (r.scope === "product") return String(r.scopeId) === String(ctx.goodsId);
    if (r.scope === "category") return ctx.dirIds.map(String).includes(String(r.scopeId));
    if (r.scope === "platform") return String(r.scopeId) === ctx.platform;
    return false;
  });
  if (matched.length === 0) return { price: applyRounding(cost, "round2"), ruleId: null };
  matched.sort(
    (a, b) => SPECIFICITY[b.scope] - SPECIFICITY[a.scope] || b.priority - a.priority,
  );
  const rule = matched[0];
  const price = Math.max(0, applyRounding(applyFormula(cost, rule), rule.rounding));
  return { price, ruleId: rule.id };
}

/** 按当前规则重算全部商品最终售价（手动锁定优先） */
export function recomputeAllPrices(): number {
  const db = getDb();
  const rows = db
    .prepare("SELECT goods_id, cost_price, dir_ids, platform, manual_price, price_locked FROM products")
    .all() as {
    goods_id: number;
    cost_price: number;
    dir_ids: string;
    platform: string;
    manual_price: number | null;
    price_locked: number;
  }[];
  const update = db.prepare("UPDATE products SET final_price = ? WHERE goods_id = ?");
  const tx = db.transaction(() => {
    for (const p of rows) {
      let final: number;
      if (p.price_locked === 1 && p.manual_price != null) {
        final = p.manual_price;
      } else {
        let dirIds: number[] = [];
        try {
          dirIds = JSON.parse(p.dir_ids || "[]");
        } catch {
          dirIds = [];
        }
        final = resolvePrice(p.cost_price, { goodsId: p.goods_id, dirIds, platform: p.platform }).price;
      }
      update.run(final, p.goods_id);
    }
  });
  tx();
  return rows.length;
}

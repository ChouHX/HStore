// 订单流转：已按业务要求剔除「退款中 / 已退款」等分支。
// 支付成功（下单完成）即视为履约/结算完成，不再提供退款状态。
export const ORDER_STATUS: Record<number, string> = {
  0: "未付款",
  1: "待处理",
  2: "进行中",
  3: "已完成",
  4: "已关闭",
  7: "待同步",
  8: "处理中",
};

export const CARD_STATUS: Record<number, string> = {
  0: "未使用",
  1: "已处理",
  2: "已作废",
  3: "已使用",
  4: "作废中",
};

export function orderStatusColor(
  status: number,
): "default" | "success" | "warning" | "danger" | "accent" {
  if (status === 3) return "success";
  if (status === 4) return "danger";
  if (status === 1 || status === 2 || status === 7 || status === 8) return "accent";
  return "default";
}

export function cardStatusColor(
  status: number,
): "default" | "success" | "warning" | "danger" | "accent" {
  if (status === 2) return "danger";
  if (status === 3) return "default";
  if (status === 4) return "warning";
  return "success";
}

/** 语义色 -> Arco Tag 颜色 */
export function arcoColor(
  c: "default" | "success" | "warning" | "danger" | "accent",
): string {
  switch (c) {
    case "success":
      return "green";
    case "danger":
      return "red";
    case "warning":
      return "orange";
    case "accent":
      return "arcoblue";
    default:
      return "gray";
  }
}

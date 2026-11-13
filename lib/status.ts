// 发货状态与买家退款分开：供货退款并不代表买家已收到退款。
export const ORDER_STATUS: Record<number, string> = {
  0: "未付款",
  1: "正在发货",
  2: "部分完成",
  3: "已完成",
  4: "发货失败",
  5: "发货已取消",
  7: "正在发货",
  8: "发货处理中",
};

export function isOrderPending(status: number | null | undefined): boolean {
  return status === 1 || status === 2 || status === 7 || status === 8;
}

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
  if (status === 4 || status === 5) return "danger";
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

/**
 * 后台轮询：自动检查进行中的 USDT 充值订单（用户关闭页面也能到账）。
 * 由 instrumentation.ts 在服务启动时调用。
 */
export function startCryptoScheduler(): void {
  const g = globalThis as unknown as { __cryptoSchedulerStarted?: boolean };
  if (g.__cryptoSchedulerStarted) return;
  g.__cryptoSchedulerStarted = true;

  const run = async () => {
    try {
      const { expireCryptoOrders, listCryptoOrders, checkCryptoOrder } = await import("./crypto");
      expireCryptoOrders();
      const waiting = listCryptoOrders({ limit: 50 })
        .filter((o) => o.status === "waiting")
        .slice(0, 10);
      for (const o of waiting) {
        await checkCryptoOrder(o.id);
      }
    } catch (err) {
      console.error("[crypto] scheduler error:", err);
    }
  };

  setTimeout(run, 15_000);
  setInterval(run, 30_000);
  console.log("[crypto] USDT 充值轮询已启动（每 30 秒）");
}

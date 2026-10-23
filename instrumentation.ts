export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return;
  const { startCryptoScheduler } = await import("./lib/crypto-scheduler");
  startCryptoScheduler();
}

import { Button } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { ProductBrowser } from "@/components/ProductBrowser";
import type { StoreHome } from "@/lib/api-types";
import { PageLoading } from "@/src/components/PageLoading";

export function HomePage() {
  const [data, setData] = useState<StoreHome | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setError("");
    try {
      const j = await fetch("/api/store/home").then((r) => r.json());
      if (j.ok) setData(j as StoreHome);
      else setError(j.message ?? "加载失败");
    } catch {
      setError("网络错误，请稍后重试");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (error) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-3">
        <div className="text-[14px] text-[#f53f3f]">{error}</div>
        <Button size="small" onClick={load}>
          重试
        </Button>
      </div>
    );
  }

  if (!data) return <PageLoading tip="正在加载商城…" />;

  return (
    <ProductBrowser
      boards={data.boards ?? []}
      configured={data.configured}
      lastSync={data.lastSync || null}
      user={data.user}
      discount={data.discount ?? 1}
      announcements={data.announcements ?? []}
      popup={data.popup?.[0] ?? null}
    />
  );
}

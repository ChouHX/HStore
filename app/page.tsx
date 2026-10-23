import { ProductBrowser } from "@/components/ProductBrowser";
import { getPublicAnnouncements } from "@/lib/announcements";
import { getMeta } from "@/lib/db";
import { loadDemoData } from "@/lib/demo-data";
import { countProducts, getStoreBoards } from "@/lib/queries";
import { getSessionUser } from "@/lib/session";
import { getLevelDiscount } from "@/lib/users";
import { isConfigured } from "@/lib/tianqi";

export const dynamic = "force-dynamic";

export default async function Page() {
  if (countProducts() === 0) loadDemoData();

  const user = await getSessionUser();
  const discount = user ? getLevelDiscount(user.levelId) : 1;
  const boards = getStoreBoards();
  const { normal, popup } = getPublicAnnouncements();

  const lastSyncRaw = getMeta("last_sync");
  const lastSync = lastSyncRaw ? Number(lastSyncRaw) : null;

  return (
    <ProductBrowser
      boards={boards}
      configured={isConfigured()}
      lastSync={lastSync}
      user={user}
      discount={discount}
      announcements={normal}
      popup={popup}
    />
  );
}

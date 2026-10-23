import { getDb, setMeta } from "./db";
import { detectPlatform } from "./platform";
import type { Platform } from "./types";

interface DemoGoods {
  name: string;
  price: number;
  face?: number;
  warranty: number;
  status?: number;
  isRepeat?: number;
  isRefOrder?: number;
  isRefMoney?: number;
}

const MICROSOFT: DemoGoods[] = [
  { name: "微软邮箱 Outlook 随机账号（全新注册）", price: 1.2, warranty: 24, isRefOrder: 1, isRefMoney: 1 },
  { name: "微软邮箱 Outlook 已绑辅助邮箱", price: 2.5, warranty: 72, isRefOrder: 1 },
  { name: "Hotmail 老号 2018 年注册 无手机", price: 6.8, warranty: 168, isRepeat: 1, isRefMoney: 1 },
  { name: "Outlook 独享号 可改密 长期稳定", price: 9.9, warranty: 720, isRefOrder: 1, isRefMoney: 1 },
  { name: "微软邮箱 批量拿货 10 个起", price: 1.0, warranty: 12, isRepeat: 1 },
  { name: "Outlook 成品号 已开通两步验证", price: 4.5, warranty: 72, isRefOrder: 1 },
  { name: "Hotmail 白号 秒发 支持 API", price: 1.8, warranty: 24, isRepeat: 1, isRefMoney: 1 },
  { name: "微软企业邮箱 Office365 子账号", price: 15.0, warranty: 1440, isRefOrder: 1, isRefMoney: 1 },
  { name: "Outlook 邮箱 带密保 高权重", price: 7.5, warranty: 168 },
  { name: "微软邮箱 停售示例（暂停）", price: 2.0, warranty: 24, status: 2 },
  { name: "Hotmail 随机号 支持换绑", price: 2.2, warranty: 48, isRefOrder: 1 },
  { name: "微软邮箱 长期稳定 秒发秒提", price: 3.6, warranty: 96, isRepeat: 1, isRefMoney: 1 },
];

const GOOGLE: DemoGoods[] = [
  { name: "谷歌邮箱 Gmail 随机账号（全新）", price: 1.5, warranty: 24, isRefOrder: 1, isRefMoney: 1 },
  { name: "Gmail 老号 2019 年注册 高权重", price: 5.8, warranty: 168, isRepeat: 1 },
  { name: "谷歌邮箱 已绑手机号 可辅助验证", price: 3.2, warranty: 72, isRefOrder: 1 },
  { name: "Google 企业邮箱 独享 可改密", price: 12.8, warranty: 720, isRefMoney: 1 },
  { name: "Gmail 批量号 5 个起 秒发", price: 1.1, warranty: 12, isRepeat: 1 },
  { name: "谷歌邮箱 成品号 已开通 2FA", price: 4.6, warranty: 72, isRefOrder: 1 },
  { name: "Gmail 白号 支持 API 对接", price: 1.9, warranty: 24, isRepeat: 1, isRefMoney: 1 },
  { name: "谷歌邮箱 带恢复邮箱 长期稳定", price: 6.5, warranty: 168, isRefOrder: 1 },
  { name: "Gmail 个人号 无绑定 即买即用", price: 2.4, warranty: 48 },
  { name: "谷歌邮箱 停售示例（禁售）", price: 2.0, warranty: 24, status: 3 },
  { name: "Gmail 高信誉老号 2016 年", price: 8.9, warranty: 720, isRefOrder: 1, isRefMoney: 1 },
  { name: "谷歌邮箱 秒发稳定 支持退单", price: 3.0, warranty: 96, isRepeat: 1, isRefOrder: 1 },
];

/** 生成演示数据（当未配置远端 App-Id 时，用于预览完整 UI/交互） */
export function loadDemoData(): { categories: number; products: number } {
  const db = getDb();
  const now = Date.now();

  const insertCategory = db.prepare(`
    INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
    VALUES(?, ?, '', ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET name = excluded.name, platform = excluded.platform,
      parent_id = excluded.parent_id, updated_at = excluded.updated_at
  `);

  const insertProduct = db.prepare(`
    INSERT INTO products(
      goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
      multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids,
      platform, enabled, updated_at)
    VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, 0, '', ?, ?, 1, ?)
    ON CONFLICT(goods_id) DO UPDATE SET
      name = excluded.name, face_value = excluded.face_value, sales_price = excluded.sales_price,
      cost_price = excluded.cost_price, final_price = excluded.final_price,
      warranty_days = excluded.warranty_days, status = excluded.status, is_repeat = excluded.is_repeat,
      is_ref_order = excluded.is_ref_order, is_ref_money = excluded.is_ref_money,
      dir_ids = excluded.dir_ids, platform = excluded.platform, enabled = 1, updated_at = excluded.updated_at
  `);

  // 清理演示平台的旧数据，保证可重复执行
  db.prepare("DELETE FROM products WHERE platform IN ('microsoft','google')").run();
  db.prepare("DELETE FROM categories WHERE platform IN ('microsoft','google')").run();

  let products = 0;
  db.transaction(() => {
    insertCategory.run(90001, "微软邮箱", null, "microsoft", now);
    insertCategory.run(90002, "谷歌邮箱", null, "google", now);

    MICROSOFT.forEach((g, i) => {
      const id = 990001 + i;
      insertProduct.run(
        id,
        g.name,
        g.face ?? 0,
        g.price,
        g.price,
        g.price,
        1,
        g.warranty,
        g.status ?? 1,
        g.isRepeat ?? 0,
        g.isRefOrder ?? 0,
        g.isRefMoney ?? 0,
        JSON.stringify([90001]),
        "microsoft",
        now,
      );
      products++;
    });

    GOOGLE.forEach((g, i) => {
      const id = 991001 + i;
      insertProduct.run(
        id,
        g.name,
        g.face ?? 0,
        g.price,
        g.price,
        g.price,
        1,
        g.warranty,
        g.status ?? 1,
        g.isRepeat ?? 0,
        g.isRefOrder ?? 0,
        g.isRefMoney ?? 0,
        JSON.stringify([90002]),
        "google",
        now,
      );
      products++;
    });
  })();

  setMeta("last_sync", String(now));
  setMeta("last_source", "demo");
  return { categories: 2, products };
}

export { detectPlatform };
export type { Platform };

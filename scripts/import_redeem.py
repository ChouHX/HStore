#!/usr/bin/env python3
"""
把 Redeem 的兑换码与自营库存迁入 HStore。

规则（2026-11 定稿）：
- 未使用的兑换码全部迁移，原码原样保留；
- 已兑换的只迁移最近 24 小时（即还能访问的）记录，生成历史订单+卡密；
- 只有「双令牌绑定辅邮」(type 18) 的可用库存迁成自营库存；
- 其他类型兑换时直接走上游商品：#5768 / #5769 / #5500。

运行前先停掉 hstore 容器，迁移完成后再启动。
python3 import_redeem.py --redeem /path/outlook_manager.db --hstore /path/faka.db [--dry-run]
"""
import argparse
import json
import random
import sqlite3
import time
from datetime import datetime, timedelta, timezone

SELF_TYPE_ID = 18
SELF_GOODS_ID = 900018
TYPE_GOODS = {1: 5768, 2: 5769, 5: 5500, 18: SELF_GOODS_ID}
TYPE_PLATFORM = {1: "microsoft", 2: "microsoft", 5: "microsoft", 18: "self"}


def ms(dt):
    if not dt:
        return 0
    try:
        v = datetime.strptime(dt, "%Y-%m-%d %H:%M:%S").replace(tzinfo=timezone.utc)
        return int(v.timestamp() * 1000)
    except Exception:
        return 0


def now_ms():
    return int(time.time() * 1000)


def new_order_no(hdb):
    for _ in range(50):
        no = "HSM%d%04d" % (int(time.time() * 1000), random.randint(1000, 9999))
        if not hdb.execute("select 1 from orders where order_number=?", (no,)).fetchone():
            return no
    raise RuntimeError("生成订单号失败")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--redeem", required=True)
    ap.add_argument("--hstore", required=True)
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    rdb = sqlite3.connect("file:%s?mode=ro" % args.redeem, uri=True)
    rdb.row_factory = sqlite3.Row
    hdb = sqlite3.connect(args.hstore)
    hdb.row_factory = sqlite3.Row

    cols = {r[1] for r in hdb.execute("pragma table_info(products)")}
    if "fulfillment" not in cols:
        raise SystemExit("HStore 数据库缺少 fulfillment 字段：请先部署新版服务并启动一次以完成建表")

    types = {r["id"]: r for r in rdb.execute("select * from redeem_email_types")}
    summary = {"stock": 0, "codes_unused": 0, "codes_used24": 0, "orders": 0, "cards": 0, "skipped_codes": 0}

    # 1) 自营商品（双令牌绑定辅邮）
    t18 = types.get(SELF_TYPE_ID)
    if not t18:
        raise SystemExit("Redeem 中找不到 type 18")
    available = rdb.execute(
        "select count(*) from redeem_inventory where type_id=? and status='available'", (SELF_TYPE_ID,)
    ).fetchone()[0]
    exists = hdb.execute("select goods_id from products where goods_id=?", (SELF_GOODS_ID,)).fetchone()
    if not exists:
        hdb.execute(
            """insert into products(goods_id,name,face_value,sales_price,cost_price,final_price,goods_type,
               warranty_days,status,multiple,is_repeat,is_ref_order,is_ref_money,sku_type,img_url,dir_ids,
               platform,root_category_id,updated_at,detail_json,detail_synced_at,stock_count,enabled,
               fulfillment,fallback_goods_id)
               values(?,?,0,0,0,0,1,0,1,1,0,0,0,0,'','[]','self',null,?, '',0,?,0,'self',0)""",
            (SELF_GOODS_ID, "自营-" + t18["name"], now_ms(), available),
        )
    else:
        hdb.execute(
            "update products set stock_count=?, fulfillment='self' where goods_id=?",
            (available, SELF_GOODS_ID),
        )

    def inv_line(payload_json, serialized_value):
        """兼容两代 schema：redeem-go 用 serialized_value，老 Node 版存 payload_json.raw_line"""
        if serialized_value:
            return serialized_value.strip()
        try:
            return (json.loads(payload_json).get("raw_line") or "").strip()
        except Exception:
            return ""

    # 2) type18 可用库存 -> self_stock（两代 schema 的列不同，按存在性拼 SQL）
    inv_cols = {r[1] for r in rdb.execute("pragma table_info(redeem_inventory)")}
    inv_sel = "id, serialized_value" + (", payload_json" if "payload_json" in inv_cols else "")
    for inv in rdb.execute(
        f"select {inv_sel} from redeem_inventory where type_id=? and status='available'",
        (SELF_TYPE_ID,),
    ):
        line = inv_line(
            inv["payload_json"] if "payload_json" in inv.keys() else None,
            inv["serialized_value"] if "serialized_value" in inv.keys() else None,
        )
        if not line:
            continue
        hdb.execute(
            "insert or ignore into self_stock(goods_id,content,status,source,source_id,created_at) values(?,?,?,?,?,?)",
            (SELF_GOODS_ID, line, "available", "redeem", str(inv["id"]), now_ms()),
        )
        summary["stock"] += 1

    # 3) 兑换码批
    batch = hdb.execute("select id from redeem_batches where name='Redeem 迁移'").fetchone()
    if batch:
        batch_id = batch["id"]
        hdb.execute("update redeem_batches set count=count where id=?", (batch_id,))
    else:
        cur = hdb.execute(
            "insert into redeem_batches(name,face_value,count,used_count,status,created_at) values('Redeem 迁移',0,0,0,'active',?)",
            (now_ms(),),
        )
        batch_id = cur.lastrowid

    def upsert_code(code, type_id, quantity, status, order_number, used_at_ms):
        goods_id = TYPE_GOODS.get(type_id)
        if not goods_id:
            summary["skipped_codes"] += 1
            return False
        cur = hdb.execute(
            """insert or ignore into redeem_codes(batch_id,code,face_value,goods_id,goods_count,order_number,
               status,used_at,created_at) values(?,?,0,?,?,?,?,?,?)""",
            (batch_id, code, goods_id, max(1, int(quantity or 1)), order_number, status, used_at_ms, now_ms()),
        )
        if cur.rowcount == 0:
            summary["skipped_codes"] += 1
            return False
        return True

    # 4) 未使用码全部迁移
    for row in rdb.execute("select * from redeem_codes where status='unused' order by id"):
        if upsert_code(row["code"], row["type_id"], row["quantity"], "unused", "", None):
            summary["codes_unused"] += 1

    # 5) 最近 24 小时已兑换：生成历史订单/卡密，原码继续可用
    cutoff = datetime.now(timezone.utc) - timedelta(hours=24)
    for row in rdb.execute(
        "select * from redeem_codes where status='redeemed' and redeemed_at >= ? order by id",
        (cutoff.strftime("%Y-%m-%d %H:%M:%S"),),
    ):
        type_id = row["type_id"]
        goods_id = TYPE_GOODS.get(type_id)
        if not goods_id:
            summary["skipped_codes"] += 1
            continue
        records = list(
            rdb.execute(
                """select r.*, i.serialized_value as inv_value
                   from redeem_records r left join redeem_inventory i on i.id = r.inventory_id
                   where r.code_id=? order by r.id""",
                (row["id"],),
            )
        )
        if not records:
            continue
        order_no = new_order_no(hdb)
        created = ms(row["redeemed_at"]) or now_ms()
        tname = types[type_id]["name"] if type_id in types else "Redeem 迁移"
        hdb.execute(
            """insert into orders(order_number,outer_number,goods_id,goods_name,count,money,status,result,
               platform,created_at,updated_at,sale_money,cost_money,source,user_uid,contact)
               values(?,?,?,?,?,0,3,?,?,?,?,0,0,'migrate',null,'')""",
            (order_no, "", goods_id, tname, len(records), "Redeem 迁移历史订单",
             TYPE_PLATFORM.get(type_id, "microsoft"), created, created),
        )
        for i, rec in enumerate(records):
            raw = rec["payload_raw"] if "payload_raw" in rec.keys() else None
            if raw and len(raw) > 3:
                line = raw.strip()
            elif "payload_json" in rec.keys() and rec["payload_json"]:
                try:
                    line = (json.loads(rec["payload_json"]).get("raw_line") or "").strip()
                except Exception:
                    line = ""
            else:
                line = (rec["inv_value"] or "").strip() if rec["inv_value"] else ""
            if not line:
                continue
            hdb.execute(
                """insert or ignore into cards(card_id,order_number,card_no,card_pwd,status,created_at)
                   values(?,?,?,'',0,?)""",
                ("mig-%s-%d" % (rec["id"], i), order_no, line, created),
            )
            summary["cards"] += 1
        if upsert_code(row["code"], type_id, len(records), "used", order_no, created):
            summary["codes_used24"] += 1
            summary["orders"] += 1

    hdb.execute(
        "update redeem_batches set count=(select count(*) from redeem_codes where batch_id=?), used_count=(select count(*) from redeem_codes where batch_id=? and status='used') where id=?",
        (batch_id, batch_id, batch_id),
    )

    if args.dry_run:
        hdb.rollback()
        print("DRY RUN，未写入")
    else:
        hdb.commit()
    print(summary)


if __name__ == "__main__":
    main()

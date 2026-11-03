#!/usr/bin/env python3
"""
把 Redeem 的兑换码与自营库存迁入 HStore。

规则（2026-11 定稿）：
- 只迁移「未使用」的兑换码，原码原样保留；
- 已兑换的码不生成历史订单/卡密，只校正本地状态（Redeem 已兑换而本地仍是 unused 的，
  改成 used），避免同一个码被兑换两次；
- 只有「双令牌绑定辅邮」(type 18) 的可用库存迁成自营库存；
- 其他类型兑换时直接走上游商品：#5768 / #5769 / #5500（这些商品不再属于自营）。

Redeem 站点保留约一周供用户自行迁移，之后下线；届时本脚本不再需要运行。

运行前先停掉 hstore 容器，迁移完成后再启动。
python3 import_redeem.py --redeem /path/outlook_manager.db --hstore /path/faka.db [--dry-run]
"""
import argparse
import json
import sqlite3
import time
from datetime import datetime, timezone

SELF_TYPE_ID = 18
SELF_GOODS_ID = 900018
TYPE_GOODS = {1: 5768, 2: 5769, 5: 5500, 18: SELF_GOODS_ID}


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
    summary = {
        "stock": 0,
        "codes_unused": 0,
        "codes_marked_used": 0,
        "skipped_codes": 0,
    }

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
    else:
        cur = hdb.execute(
            "insert into redeem_batches(name,face_value,count,used_count,status,created_at) values('Redeem 迁移',0,0,0,'active',?)",
            (now_ms(),),
        )
        batch_id = cur.lastrowid

    def upsert_code(code, type_id, quantity):
        goods_id = TYPE_GOODS.get(type_id)
        if not goods_id:
            summary["skipped_codes"] += 1
            return False
        cur = hdb.execute(
            """insert or ignore into redeem_codes(batch_id,code,face_value,goods_id,goods_count,order_number,
               status,used_at,created_at) values(?,?,0,?,?,'','unused',null,?)""",
            (batch_id, code, goods_id, max(1, int(quantity or 1)), now_ms()),
        )
        if cur.rowcount == 0:
            summary["skipped_codes"] += 1
            return False
        return True

    # 4) 未使用码全部迁移（已存在的码不动，保持幂等）
    for row in rdb.execute("select * from redeem_codes where status='unused' order by id"):
        if upsert_code(row["code"], row["type_id"], row["quantity"]):
            summary["codes_unused"] += 1

    # 5) 已兑换的码：不建订单/卡密，只把本地仍是 unused 的改成 used。
    #
    # 之前一版脚本会把「最近 24 小时已兑换」的记录做成历史订单+卡密，但用户拿到的
    # 卡密和自营库存是同一批，等于把同一份库存发了两遍，所以这条路径整体去掉。
    # 这里只做状态校正：Redeem 侧已兑换、本地还显示未使用的码必须置为 used，
    # 否则用户能在 HStore 再兑一次（实际会多发出一份货）。
    for row in rdb.execute("select * from redeem_codes where status='redeemed' order by id"):
        used_at = ms(row["redeemed_at"]) or now_ms()
        cur = hdb.execute(
            "update redeem_codes set status='used', used_at=? where code=? and status='unused'",
            (used_at, row["code"]),
        )
        if cur.rowcount > 0:
            summary["codes_marked_used"] += 1

    hdb.execute(
        "update redeem_batches set count=(select count(*) from redeem_codes where batch_id=?), used_count=(select count(*) from redeem_codes where batch_id=? and status='used') where id=?",
        (batch_id, batch_id, batch_id),
    )

    # 6) 反向风险：本地已兑换、Redeem 侧还显示「未使用」的码。
    #
    # 用户在 HStore 已经兑换取件了，但码在 Redeem 站点仍然可用，等于同一份货能领两次。
    # 这类码必须在 Redeem 侧封禁（status='disabled'）。Redeem 库这里是只读打开的，
    # 所以只检测并打印，由管理员通过 Redeem 后台/接口封禁：
    #   POST /api/redeem/admin/codes/{id}/status  {"status":"disabled"}
    reverse_risk = []
    for row in hdb.execute("select code from redeem_codes where status='used'"):
        r = rdb.execute(
            "select id from redeem_codes where code=? and status='unused'", (row["code"],)
        ).fetchone()
        if r:
            reverse_risk.append({"id": r["id"], "code": row["code"]})
    if reverse_risk:
        print("!! 以下兑换码已在 HStore 兑换，但 Redeem 侧仍可兑换，请在 Redeem 后台封禁：")
        for it in reverse_risk:
            print("   id=%(id)s  %(code)s" % it)

    if args.dry_run:
        hdb.rollback()
        print("DRY RUN，未写入")
    else:
        hdb.commit()
    print(summary)


if __name__ == "__main__":
    main()

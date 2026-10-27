package store

import "hstore/backend/internal/db"

// ProductImageURLs 返回所有商品的上游图片地址（去重，最多 500 个），用于同步后预热缓存。
func ProductImageURLs() []string {
	rows, err := db.Must().Query(`SELECT DISTINCT img_url FROM products WHERE img_url LIKE 'http%'
		UNION SELECT DISTINCT img FROM categories WHERE img LIKE 'http%' LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil && u != "" {
			out = append(out, u)
		}
	}
	return out
}

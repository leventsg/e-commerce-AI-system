-- 商城演示数据。
-- 字段列表已按开发容器中的 MySQL 实际表结构对齐。

USE mall;

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

START TRANSACTION;

-- 商品分类。
INSERT INTO categories (id, name, description) VALUES
    (1, '手机数码', '手机、耳机、智能穿戴与数码配件'),
    (2, '电脑办公', '笔记本、显示器、键鼠与办公设备'),
    (3, '家用电器', '厨房电器、生活电器与清洁设备'),
    (4, '食品生鲜', '零食、饮品、生鲜与粮油调味'),
    (5, '服饰鞋包', '服装、鞋靴、箱包与配饰'),
    (6, '家居日用', '清洁、收纳、厨具与家纺')
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    description = VALUES(description),
    updated_at = CURRENT_TIMESTAMP;

-- 商品。价格单位：分。
INSERT INTO products (id, name, description, picture, price) VALUES
    (1001, '星云 X1 智能手机 12GB+256GB', '6.7 英寸高刷屏，5000mAh 电池，支持 5G 双卡双待', 'https://example.com/assets/products/nebula-x1.jpg', 399900),
    (1002, '声澜 Pro 主动降噪耳机', '自适应降噪，空间音频，单次续航 8 小时', 'https://example.com/assets/products/soundcore-pro.jpg', 89900),
    (1003, '轻薄本 Air 14 16GB+1TB', '14 英寸 2.8K 屏，金属机身，适合办公与学习', 'https://example.com/assets/products/air14.jpg', 569900),
    (1004, '27 英寸 4K 办公显示器', 'IPS 面板，Type-C 反向供电，低蓝光模式', 'https://example.com/assets/products/monitor-27-4k.jpg', 169900),
    (1005, '智能空气炸锅 5L', '可视化窗口，八种菜单，支持预约烹饪', 'https://example.com/assets/products/air-fryer-5l.jpg', 49900),
    (1006, '全自动扫拖机器人 S7', '激光导航，自动回充，支持智能分区清扫', 'https://example.com/assets/products/robot-s7.jpg', 229900),
    (1007, '精品挂耳咖啡 30 包', '中深烘焙，坚果与可可风味，独立包装', 'https://example.com/assets/products/drip-coffee.jpg', 12900),
    (1008, '有机东北大米 5kg', '当季新米，颗粒饱满，适合家庭日常主食', 'https://example.com/assets/products/rice-5kg.jpg', 7900),
    (1009, '城市通勤防泼水双肩包', '15.6 英寸电脑隔层，多口袋收纳，防泼水面料', 'https://example.com/assets/products/backpack.jpg', 23900),
    (1010, '纯棉基础款短袖 T 恤', '230g 重磅纯棉，宽松版型，多色可选', 'https://example.com/assets/products/tshirt.jpg', 9900),
    (1011, '加厚真空保温杯 480ml', '316 不锈钢内胆，长效保温，车载杯架适配', 'https://example.com/assets/products/thermos.jpg', 8900),
    (1012, '可折叠收纳箱 45L', '透明可视，侧开门设计，可多层叠放', 'https://example.com/assets/products/storage-box.jpg', 6900)
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    description = VALUES(description),
    picture = VALUES(picture),
    price = VALUES(price),
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO product_categories (product_id, category_id) VALUES
    (1001, 1),
    (1002, 1),
    (1003, 2),
    (1004, 2),
    (1005, 3),
    (1006, 3),
    (1007, 4),
    (1008, 4),
    (1009, 5),
    (1010, 5),
    (1011, 6),
    (1012, 6)
ON DUPLICATE KEY UPDATE
    product_id = VALUES(product_id),
    category_id = VALUES(category_id);

-- 库存。可售库存 = total - sold。
INSERT INTO inventory (product_id, total, sold) VALUES
    (1001, 120, 8),
    (1002, 300, 26),
    (1003, 80, 5),
    (1004, 100, 11),
    (1005, 180, 20),
    (1006, 60, 4),
    (1007, 500, 77),
    (1008, 400, 38),
    (1009, 220, 18),
    (1010, 360, 42),
    (1011, 260, 31),
    (1012, 320, 29)
ON DUPLICATE KEY UPDATE
    total = VALUES(total),
    sold = VALUES(sold);

-- 优惠券。
-- type: 1 = 满减券，2 = 折扣券（90 表示 9 折），3 = 立减券。
-- 固定金额类 value/min_amount 单位：分。
INSERT INTO coupons (
    id, name, type, value, min_amount, start_time, end_time, status, total_count, remaining_count
) VALUES
    ('CPN-NEW-0500', '新人立减 5 元券', 3, 500, 0, '2026-01-01 00:00:00', '2026-12-31 23:59:59', 1, 10000, 9999),
    ('CPN-MAN-3000-300', '满 30 减 3 元券', 1, 300, 3000, '2026-01-01 00:00:00', '2026-12-31 23:59:59', 1, 8000, 7999),
    ('CPN-MAN-10000-1500', '满 100 减 15 元券', 1, 1500, 10000, '2026-01-01 00:00:00', '2026-12-31 23:59:59', 1, 5000, 5000),
    ('CPN-DISCOUNT-90', '全场 9 折券', 2, 90, 0, '2026-01-01 00:00:00', '2026-12-31 23:59:59', 1, 3000, 2999),
    ('CPN-DIGITAL-20000-3000', '数码满 200 减 30 元券', 1, 3000, 20000, '2026-01-01 00:00:00', '2026-12-31 23:59:59', 1, 2000, 2000),
    ('CPN-OFFLINE-EXPIRED', '已过期测试券', 3, 1000, 0, '2025-01-01 00:00:00', '2025-12-31 23:59:59', 1, 1000, 1000),
    ('CPN-DISABLED-TEST', '已禁用测试券', 3, 800, 0, '2026-01-01 00:00:00', '2026-12-31 23:59:59', 0, 1000, 1000)
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    type = VALUES(type),
    value = VALUES(value),
    min_amount = VALUES(min_amount),
    start_time = VALUES(start_time),
    end_time = VALUES(end_time),
    status = VALUES(status),
    total_count = VALUES(total_count),
    remaining_count = VALUES(remaining_count),
    updated_at = CURRENT_TIMESTAMP;


COMMIT;

SET FOREIGN_KEY_CHECKS = 1;

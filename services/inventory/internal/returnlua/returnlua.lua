-- KEYS[1]：预扣库存标记 Key，用于标识该订单已完成预扣库存，可执行库存回滚  lock-key
-- KEYS[2]：库存归还标记 Key，用于标识该订单的预扣库存已经归还 returned-key
-- KEYS[3...n]：商品库存 Key
--
-- ARGV[1]：pre_order_id（预订单 ID）
-- ARGV[2...n-1]：各商品对应的归还数量
--
-- 如果库存已经归还，则视为幂等操作，直接返回成功
if redis.call("EXISTS", KEYS[2]) == 1 then
    return 1
end

-- 预扣库存标记缺失且归还标记缺失
-- 无法确认归还状态是否安全
if redis.call("EXISTS", KEYS[1]) ~= 1 then
    return 2
end

-- 预扣库存回滚
for i=3, #KEYS do
    redis.call("INCRBY", KEYS[i], tonumber(ARGV[i - 1]))
end

redis.call("DEL", KEYS[1])
-- TTL: 24 小时，过期后无法再执行归还操作，避免误操作导致库存异常
redis.call("SET", KEYS[2], ARGV[1], "EX", 86400)

return 0

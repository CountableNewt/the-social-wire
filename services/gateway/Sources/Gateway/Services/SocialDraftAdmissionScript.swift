enum SocialDraftAdmissionScript {
  static let acquire = #"""
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+tonumber(clock[2])/1000
local upload=ARGV[1]=='1'
local function bucket(key,capacity,rate)
 local v=redis.call('HMGET',key,'tokens','updated_at')
 local tokens=tonumber(v[1]) or capacity
 local updated=tonumber(v[2]) or now
 return math.min(capacity,tokens+math.max(0,now-updated)*rate)
end
local requests=bucket(KEYS[1],120,0.01)
if requests<1 then return 0 end
local uploads=0
if upload then
 uploads=bucket(KEYS[2],32,0.001)
 if uploads<1 then return 0 end
 redis.call('ZREMRANGEBYSCORE',KEYS[3],'-inf',now)
 redis.call('ZREMRANGEBYSCORE',KEYS[4],'-inf',now)
 if redis.call('ZCARD',KEYS[3])>=8 or redis.call('ZCARD',KEYS[4])>=2 then return 0 end
 local expires=now+tonumber(ARGV[3])
 redis.call('ZADD',KEYS[3],expires,ARGV[2])
 redis.call('ZADD',KEYS[4],expires,ARGV[2])
 redis.call('PEXPIRE',KEYS[3],ARGV[3])
 redis.call('PEXPIRE',KEYS[4],ARGV[3])
 redis.call('HSET',KEYS[2],'tokens',uploads-1,'updated_at',now)
 redis.call('PEXPIRE',KEYS[2],120000)
end
redis.call('HSET',KEYS[1],'tokens',requests-1,'updated_at',now)
redis.call('PEXPIRE',KEYS[1],120000)
return 1
"""#
}

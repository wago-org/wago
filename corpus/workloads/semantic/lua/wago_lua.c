#include <stdint.h>
#include "lua.h"
#include "lauxlib.h"
#include "lualib.h"

static const char program[] =
    "local function primes(n) "
    "  local p={} for i=2,n do p[i]=true end "
    "  for i=2,32 do if p[i] then for j=i*i,n,i do p[j]=false end end end "
    "  local out={} for i=2,n do if p[i] then out[#out+1]=i end end return out end "
    "local values=primes(1000); local sum,count=0,#values "
    "for i=1,#values do sum=sum+values[i] end "
    "local function scale(k) return function(v) return v*k end end; sum=scale(3)(sum) "
    "local s=('wago-webassembly-runtime'):gsub('[aeiou]','_') "
    "return sum*1000+count+#s";

uint64_t lua_run(void) {
    lua_State *L = luaL_newstate();
    if (!L) return 0;
    luaL_requiref(L, LUA_STRLIBNAME, luaopen_string, 1); lua_pop(L, 1);
    if (luaL_loadbufferx(L, program, sizeof(program) - 1, "corpus", "t") != LUA_OK ||
        lua_pcall(L, 0, 1, 0) != LUA_OK) {
        lua_close(L);
        return 0;
    }
    lua_Integer result = lua_tointeger(L, -1);
    lua_gc(L, LUA_GCCOLLECT);
    lua_close(L);
    return (uint64_t)result;
}

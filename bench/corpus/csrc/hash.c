// hash: FNV-1a-style mixing over an LCG stream of 64-bit words. Integer/bitwise
// heavy (i64 mul/xor/shift), no memory.
long hashmix(int n){ unsigned long long s=0xcbf29ce484222325ULL, lcg=0x853c49e6748fea9bULL; for(int i=0;i<n;i++){ lcg=lcg*6364136223846793005ULL+1442695040888963407ULL; s^=lcg; s*=0x100000001b3ULL; s^=s>>33; } return (long)s; }

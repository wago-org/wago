// geo: a small freestanding stand-in for a GEOS-style geometry workload.
// Generates a jittered polygon point cloud into static memory, then computes
// shoelace area, perimeter, centroid distance and a point-in-polygon ray cast.
// Doubles + static buffers only (no libc, no malloc) so it stays inside wago's
// compilable opcode set; its own sin/cos polynomials avoid libm calls.
#define MAXN 2048
static double xs[MAXN], ys[MAXN];
static unsigned long long seed;
static double rnd(void){ seed = seed*6364136223846793005ULL + 1442695040888963407ULL; return (double)(seed>>11)*(1.0/9007199254740992.0); }
static double sina(double x){ while(x>3.14159265358979) x-=6.28318530717959; while(x<-3.14159265358979) x+=6.28318530717959; double x2=x*x; return x*(1.0+x2*(-0.16666667+x2*(0.0083333+x2*(-0.00019841)))); }
static double cosa(double x){ return sina(x+1.5707963267948966); }
static int clampn(int n){ if(n<3) n=3; if(n>MAXN) n=MAXN; return n; }
static void gen(int n){ seed=0x9e3779b97f4a7c15ULL; for(int i=0;i<n;i++){ double a=6.28318530717959*i/n; double r=0.6+0.4*rnd(); xs[i]=r*cosa(a); ys[i]=r*sina(a); } }
double geo_area(int n){ n=clampn(n); gen(n); double a=0; for(int i=0,j=n-1;i<n;j=i++) a+=(xs[j]+xs[i])*(ys[j]-ys[i]); return a<0?-a*0.5:a*0.5; }
double geo_perimeter(int n){ n=clampn(n); gen(n); double p=0; for(int i=0,j=n-1;i<n;j=i++){ double dx=xs[i]-xs[j], dy=ys[i]-ys[j]; p+=__builtin_sqrt(dx*dx+dy*dy); } return p; }
int geo_pip(int n, int qxm, int qym){ n=clampn(n); gen(n); double qx=qxm*0.001, qy=qym*0.001; int in=0; for(int i=0,j=n-1;i<n;j=i++){ if(((ys[i]>qy)!=(ys[j]>qy)) && (qx < (xs[j]-xs[i])*(qy-ys[i])/(ys[j]-ys[i])+xs[i])) in=!in; } return in; }

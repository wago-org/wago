// mandel: escape-time Mandelbrot over a w x h grid, returns an iteration-sum
// checksum. Pure f64/i32 compute, no memory — exercises the FP pipeline.
int mandel(int w, int h, int maxit){ long sum=0; for(int py=0;py<h;py++){ double y0=(double)py/h*2.0-1.0; for(int px=0;px<w;px++){ double x0=(double)px/w*3.0-2.0; double x=0,y=0; int it=0; while(it<maxit){ double x2=x*x,y2=y*y; if(x2+y2>4.0) break; y=2*x*y+y0; x=x2-y2+x0; it++; } sum+=it; } } return (int)sum; }

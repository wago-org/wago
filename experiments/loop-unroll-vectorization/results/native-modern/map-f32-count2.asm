
experiments/loop-unroll-vectorization/results/native-modern/map-f32-count2.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 68 00 00 00 	sub    $0x68,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	44 8b 27             	mov    (%rdi),%r12d
  19:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  1d:	44 8b 77 10          	mov    0x10(%rdi),%r14d
  21:	f3 44 0f 10 6f 18    	movss  0x18(%rdi),%xmm13
  27:	f3 44 0f 10 77 20    	movss  0x20(%rdi),%xmm14
  2d:	31 c0                	xor    %eax,%eax
  2f:	66 45 0f 57 e4       	xorpd  %xmm12,%xmm12
  34:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3b:	00 00 
  3d:	0f 1f 00             	nopl   (%rax)
  40:	41 83 fe 02          	cmp    $0x2,%r14d
  44:	0f 82 80 00 00 00    	jb     0xca
  4a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4f:	45 89 ed             	mov    %r13d,%r13d
  52:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  56:	4c 39 ff             	cmp    %r15,%rdi
  59:	0f 87 03 01 00 00    	ja     0x162
  5f:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  65:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  6a:	45 89 e4             	mov    %r12d,%r12d
  6d:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  72:	4c 39 ff             	cmp    %r15,%rdi
  75:	0f 87 e7 00 00 00    	ja     0x162
  7b:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  81:	41 83 c4 04          	add    $0x4,%r12d
  85:	41 83 c5 04          	add    $0x4,%r13d
  89:	41 83 ee 01          	sub    $0x1,%r14d
  8d:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  91:	4c 39 ff             	cmp    %r15,%rdi
  94:	0f 87 c8 00 00 00    	ja     0x162
  9a:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  a0:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  a5:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  aa:	4c 39 ff             	cmp    %r15,%rdi
  ad:	0f 87 af 00 00 00    	ja     0x162
  b3:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  b9:	41 83 c4 04          	add    $0x4,%r12d
  bd:	41 83 c5 04          	add    $0x4,%r13d
  c1:	41 83 ee 01          	sub    $0x1,%r14d
  c5:	e9 76 ff ff ff       	jmp    0x40
  ca:	66 0f 1f 44 00 00    	nopw   0x0(%rax,%rax,1)
  d0:	45 85 f6             	test   %r14d,%r14d
  d3:	0f 84 43 00 00 00    	je     0x11c
  d9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  de:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  e2:	4c 39 ff             	cmp    %r15,%rdi
  e5:	0f 87 77 00 00 00    	ja     0x162
  eb:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  f1:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  f6:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  fb:	4c 39 ff             	cmp    %r15,%rdi
  fe:	0f 87 5e 00 00 00    	ja     0x162
 104:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
 10a:	41 83 c4 04          	add    $0x4,%r12d
 10e:	41 83 c5 04          	add    $0x4,%r13d
 112:	41 83 ee 01          	sub    $0x1,%r14d
 116:	0f 85 c2 ff ff ff    	jne    0xde
 11c:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 121:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
 126:	4c 89 74 24 48       	mov    %r14,0x48(%rsp)
 12b:	f2 44 0f 11 64 24 50 	movsd  %xmm12,0x50(%rsp)
 132:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 137:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
 13c:	48 89 07             	mov    %rax,(%rdi)
 13f:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 144:	48 89 47 08          	mov    %rax,0x8(%rdi)
 148:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 14d:	48 89 47 10          	mov    %rax,0x10(%rdi)
 151:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 156:	48 89 47 18          	mov    %rax,0x18(%rdi)
 15a:	48 81 c4 68 00 00 00 	add    $0x68,%rsp
 161:	c3                   	ret
 162:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 167:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 16b:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 172:	89 46 14             	mov    %eax,0x14(%rsi)
 175:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 17b:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 17f:	c3                   	ret

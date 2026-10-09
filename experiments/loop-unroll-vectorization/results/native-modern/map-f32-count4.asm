
experiments/loop-unroll-vectorization/results/native-modern/map-f32-count4.bin:     file format binary


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
  40:	41 83 fe 04          	cmp    $0x4,%r14d
  44:	0f 82 f0 00 00 00    	jb     0x13a
  4a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4f:	45 89 ed             	mov    %r13d,%r13d
  52:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  56:	4c 39 ff             	cmp    %r15,%rdi
  59:	0f 87 7b 01 00 00    	ja     0x1da
  5f:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  65:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  6a:	45 89 e4             	mov    %r12d,%r12d
  6d:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  72:	4c 39 ff             	cmp    %r15,%rdi
  75:	0f 87 5f 01 00 00    	ja     0x1da
  7b:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  81:	41 83 c4 04          	add    $0x4,%r12d
  85:	41 83 c5 04          	add    $0x4,%r13d
  89:	41 83 ee 01          	sub    $0x1,%r14d
  8d:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  91:	4c 39 ff             	cmp    %r15,%rdi
  94:	0f 87 40 01 00 00    	ja     0x1da
  9a:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  a0:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  a5:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  aa:	4c 39 ff             	cmp    %r15,%rdi
  ad:	0f 87 27 01 00 00    	ja     0x1da
  b3:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  b9:	41 83 c4 04          	add    $0x4,%r12d
  bd:	41 83 c5 04          	add    $0x4,%r13d
  c1:	41 83 ee 01          	sub    $0x1,%r14d
  c5:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  c9:	4c 39 ff             	cmp    %r15,%rdi
  cc:	0f 87 08 01 00 00    	ja     0x1da
  d2:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  d8:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  dd:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  e2:	4c 39 ff             	cmp    %r15,%rdi
  e5:	0f 87 ef 00 00 00    	ja     0x1da
  eb:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  f1:	41 83 c4 04          	add    $0x4,%r12d
  f5:	41 83 c5 04          	add    $0x4,%r13d
  f9:	41 83 ee 01          	sub    $0x1,%r14d
  fd:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 101:	4c 39 ff             	cmp    %r15,%rdi
 104:	0f 87 d0 00 00 00    	ja     0x1da
 10a:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
 110:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
 115:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 11a:	4c 39 ff             	cmp    %r15,%rdi
 11d:	0f 87 b7 00 00 00    	ja     0x1da
 123:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
 129:	41 83 c4 04          	add    $0x4,%r12d
 12d:	41 83 c5 04          	add    $0x4,%r13d
 131:	41 83 ee 01          	sub    $0x1,%r14d
 135:	e9 06 ff ff ff       	jmp    0x40
 13a:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 141:	00 00 
 143:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 148:	45 85 f6             	test   %r14d,%r14d
 14b:	0f 84 43 00 00 00    	je     0x194
 151:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 156:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 15a:	4c 39 ff             	cmp    %r15,%rdi
 15d:	0f 87 77 00 00 00    	ja     0x1da
 163:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
 169:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
 16e:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 173:	4c 39 ff             	cmp    %r15,%rdi
 176:	0f 87 5e 00 00 00    	ja     0x1da
 17c:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
 182:	41 83 c4 04          	add    $0x4,%r12d
 186:	41 83 c5 04          	add    $0x4,%r13d
 18a:	41 83 ee 01          	sub    $0x1,%r14d
 18e:	0f 85 c2 ff ff ff    	jne    0x156
 194:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 199:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
 19e:	4c 89 74 24 48       	mov    %r14,0x48(%rsp)
 1a3:	f2 44 0f 11 64 24 50 	movsd  %xmm12,0x50(%rsp)
 1aa:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 1af:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
 1b4:	48 89 07             	mov    %rax,(%rdi)
 1b7:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 1bc:	48 89 47 08          	mov    %rax,0x8(%rdi)
 1c0:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 1c5:	48 89 47 10          	mov    %rax,0x10(%rdi)
 1c9:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 1ce:	48 89 47 18          	mov    %rax,0x18(%rdi)
 1d2:	48 81 c4 68 00 00 00 	add    $0x68,%rsp
 1d9:	c3                   	ret
 1da:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 1df:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 1e3:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1ea:	89 46 14             	mov    %eax,0x14(%rsi)
 1ed:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1f3:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1f7:	c3                   	ret


experiments/loop-unroll-vectorization/results/native/map-f32-count2.bin:     file format binary


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
  44:	0f 82 94 00 00 00    	jb     0xde
  4a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4f:	45 89 ed             	mov    %r13d,%r13d
  52:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  56:	4c 39 ff             	cmp    %r15,%rdi
  59:	0f 87 25 01 00 00    	ja     0x184
  5f:	f3 41 0f 10 c5       	movss  %xmm13,%xmm0
  64:	f3 42 0f 59 04 2b    	mulss  (%rbx,%r13,1),%xmm0
  6a:	f3 44 0f 10 e0       	movss  %xmm0,%xmm12
  6f:	f3 45 0f 58 e6       	addss  %xmm14,%xmm12
  74:	45 89 e4             	mov    %r12d,%r12d
  77:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  7c:	4c 39 ff             	cmp    %r15,%rdi
  7f:	0f 87 ff 00 00 00    	ja     0x184
  85:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  8b:	41 83 c4 04          	add    $0x4,%r12d
  8f:	41 83 c5 04          	add    $0x4,%r13d
  93:	41 83 ee 01          	sub    $0x1,%r14d
  97:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  9b:	4c 39 ff             	cmp    %r15,%rdi
  9e:	0f 87 e0 00 00 00    	ja     0x184
  a4:	f3 41 0f 10 c5       	movss  %xmm13,%xmm0
  a9:	f3 42 0f 59 04 2b    	mulss  (%rbx,%r13,1),%xmm0
  af:	f3 44 0f 10 e0       	movss  %xmm0,%xmm12
  b4:	f3 45 0f 58 e6       	addss  %xmm14,%xmm12
  b9:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  be:	4c 39 ff             	cmp    %r15,%rdi
  c1:	0f 87 bd 00 00 00    	ja     0x184
  c7:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  cd:	41 83 c4 04          	add    $0x4,%r12d
  d1:	41 83 c5 04          	add    $0x4,%r13d
  d5:	41 83 ee 01          	sub    $0x1,%r14d
  d9:	e9 62 ff ff ff       	jmp    0x40
  de:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  e5:	00 00 
  e7:	90                   	nop
  e8:	45 85 f6             	test   %r14d,%r14d
  eb:	0f 84 4d 00 00 00    	je     0x13e
  f1:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  f6:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  fa:	4c 39 ff             	cmp    %r15,%rdi
  fd:	0f 87 81 00 00 00    	ja     0x184
 103:	f3 41 0f 10 c5       	movss  %xmm13,%xmm0
 108:	f3 42 0f 59 04 2b    	mulss  (%rbx,%r13,1),%xmm0
 10e:	f3 44 0f 10 e0       	movss  %xmm0,%xmm12
 113:	f3 45 0f 58 e6       	addss  %xmm14,%xmm12
 118:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 11d:	4c 39 ff             	cmp    %r15,%rdi
 120:	0f 87 5e 00 00 00    	ja     0x184
 126:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
 12c:	41 83 c4 04          	add    $0x4,%r12d
 130:	41 83 c5 04          	add    $0x4,%r13d
 134:	41 83 ee 01          	sub    $0x1,%r14d
 138:	0f 85 b8 ff ff ff    	jne    0xf6
 13e:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 143:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
 148:	4c 89 74 24 48       	mov    %r14,0x48(%rsp)
 14d:	f2 44 0f 11 64 24 50 	movsd  %xmm12,0x50(%rsp)
 154:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 159:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
 15e:	48 89 07             	mov    %rax,(%rdi)
 161:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 166:	48 89 47 08          	mov    %rax,0x8(%rdi)
 16a:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 16f:	48 89 47 10          	mov    %rax,0x10(%rdi)
 173:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 178:	48 89 47 18          	mov    %rax,0x18(%rdi)
 17c:	48 81 c4 68 00 00 00 	add    $0x68,%rsp
 183:	c3                   	ret
 184:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 189:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 18d:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 194:	89 46 14             	mov    %eax,0x14(%rsi)
 197:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 19d:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1a1:	c3                   	ret

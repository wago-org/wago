
experiments/loop-unroll-vectorization/results/native-modern/map-f32-vector-f32.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 78 00 00 00 	sub    $0x78,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	44 8b 2f             	mov    (%rdi),%r13d
  19:	44 8b 77 08          	mov    0x8(%rdi),%r14d
  1d:	44 8b 67 10          	mov    0x10(%rdi),%r12d
  21:	f3 44 0f 10 6f 18    	movss  0x18(%rdi),%xmm13
  27:	f3 44 0f 10 77 20    	movss  0x20(%rdi),%xmm14
  2d:	31 c0                	xor    %eax,%eax
  2f:	66 45 0f 57 e4       	xorpd  %xmm12,%xmm12
  34:	66 45 0f 57 ff       	xorpd  %xmm15,%xmm15
  39:	8b 7b fc             	mov    -0x4(%rbx),%edi
  3c:	8b 73 fc             	mov    -0x4(%rbx),%esi
  3f:	45 89 e1             	mov    %r12d,%r9d
  42:	49 c1 e1 02          	shl    $0x2,%r9
  46:	45 89 f2             	mov    %r14d,%r10d
  49:	4d 01 ca             	add    %r9,%r10
  4c:	89 ff                	mov    %edi,%edi
  4e:	48 c1 e7 10          	shl    $0x10,%rdi
  52:	49 39 fa             	cmp    %rdi,%r10
  55:	40 0f 96 c5          	setbe  %bpl
  59:	40 0f b6 ed          	movzbl %bpl,%ebp
  5d:	44 89 e7             	mov    %r12d,%edi
  60:	48 c1 e7 02          	shl    $0x2,%rdi
  64:	45 89 f1             	mov    %r14d,%r9d
  67:	49 01 f9             	add    %rdi,%r9
  6a:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  71:	00 00 00 
  74:	49 39 f9             	cmp    %rdi,%r9
  77:	41 0f 96 c1          	setbe  %r9b
  7b:	45 0f b6 c9          	movzbl %r9b,%r9d
  7f:	44 21 cd             	and    %r9d,%ebp
  82:	44 89 e7             	mov    %r12d,%edi
  85:	48 c1 e7 02          	shl    $0x2,%rdi
  89:	45 89 e9             	mov    %r13d,%r9d
  8c:	49 01 f9             	add    %rdi,%r9
  8f:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  96:	00 00 00 
  99:	49 39 f9             	cmp    %rdi,%r9
  9c:	41 0f 96 c1          	setbe  %r9b
  a0:	45 0f b6 c9          	movzbl %r9b,%r9d
  a4:	44 21 cd             	and    %r9d,%ebp
  a7:	44 89 e7             	mov    %r12d,%edi
  aa:	48 c1 e7 02          	shl    $0x2,%rdi
  ae:	45 89 e9             	mov    %r13d,%r9d
  b1:	49 01 f9             	add    %rdi,%r9
  b4:	89 f6                	mov    %esi,%esi
  b6:	48 c1 e6 10          	shl    $0x10,%rsi
  ba:	49 39 f1             	cmp    %rsi,%r9
  bd:	41 0f 96 c1          	setbe  %r9b
  c1:	45 0f b6 c9          	movzbl %r9b,%r9d
  c5:	44 21 cd             	and    %r9d,%ebp
  c8:	44 89 f6             	mov    %r14d,%esi
  cb:	44 39 ee             	cmp    %r13d,%esi
  ce:	40 0f 94 c7          	sete   %dil
  d2:	40 0f b6 ff          	movzbl %dil,%edi
  d6:	44 89 e6             	mov    %r12d,%esi
  d9:	48 c1 e6 02          	shl    $0x2,%rsi
  dd:	45 89 f1             	mov    %r14d,%r9d
  e0:	49 01 f1             	add    %rsi,%r9
  e3:	44 89 ee             	mov    %r13d,%esi
  e6:	49 39 f1             	cmp    %rsi,%r9
  e9:	41 0f 96 c1          	setbe  %r9b
  ed:	45 0f b6 c9          	movzbl %r9b,%r9d
  f1:	44 09 cf             	or     %r9d,%edi
  f4:	44 89 e6             	mov    %r12d,%esi
  f7:	48 c1 e6 02          	shl    $0x2,%rsi
  fb:	45 89 e9             	mov    %r13d,%r9d
  fe:	49 01 f1             	add    %rsi,%r9
 101:	44 89 f6             	mov    %r14d,%esi
 104:	49 39 f1             	cmp    %rsi,%r9
 107:	41 0f 96 c1          	setbe  %r9b
 10b:	45 0f b6 c9          	movzbl %r9b,%r9d
 10f:	44 09 cf             	or     %r9d,%edi
 112:	21 fd                	and    %edi,%ebp
 114:	85 ed                	test   %ebp,%ebp
 116:	0f 84 e4 00 00 00    	je     0x200
 11c:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 123:	00 00 
 125:	0f 1f 00             	nopl   (%rax)
 128:	41 83 fc 04          	cmp    $0x4,%r12d
 12c:	0f 82 79 00 00 00    	jb     0x1ab
 132:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 137:	45 89 f6             	mov    %r14d,%r14d
 13a:	49 8d 7e 10          	lea    0x10(%r14),%rdi
 13e:	4c 39 ff             	cmp    %r15,%rdi
 141:	0f 87 4a 01 00 00    	ja     0x291
 147:	c4 a1 7a 6f 04 33    	vmovdqu (%rbx,%r14,1),%xmm0
 14d:	f3 41 0f 10 cd       	movss  %xmm13,%xmm1
 152:	66 0f 70 c9 00       	pshufd $0x0,%xmm1,%xmm1
 157:	c4 e1 78 59 c1       	vmulps %xmm1,%xmm0,%xmm0
 15c:	f3 41 0f 10 ce       	movss  %xmm14,%xmm1
 161:	66 0f 70 c9 00       	pshufd $0x0,%xmm1,%xmm1
 166:	c4 e1 78 58 c1       	vaddps %xmm1,%xmm0,%xmm0
 16b:	c4 61 7a 6f f8       	vmovdqu %xmm0,%xmm15
 170:	c4 c1 7a 6f c7       	vmovdqu %xmm15,%xmm0
 175:	45 89 ed             	mov    %r13d,%r13d
 178:	49 8d 7d 10          	lea    0x10(%r13),%rdi
 17c:	4c 39 ff             	cmp    %r15,%rdi
 17f:	0f 87 0c 01 00 00    	ja     0x291
 185:	c4 a1 7a 7f 04 2b    	vmovdqu %xmm0,(%rbx,%r13,1)
 18b:	c4 c1 7a 6f c7       	vmovdqu %xmm15,%xmm0
 190:	66 0f 70 c0 03       	pshufd $0x3,%xmm0,%xmm0
 195:	f3 44 0f 10 e0       	movss  %xmm0,%xmm12
 19a:	41 83 c5 10          	add    $0x10,%r13d
 19e:	41 83 c6 10          	add    $0x10,%r14d
 1a2:	41 83 ec 04          	sub    $0x4,%r12d
 1a6:	e9 7d ff ff ff       	jmp    0x128
 1ab:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 1b0:	45 85 e4             	test   %r12d,%r12d
 1b3:	0f 84 42 00 00 00    	je     0x1fb
 1b9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 1be:	49 8d 7e 04          	lea    0x4(%r14),%rdi
 1c2:	4c 39 ff             	cmp    %r15,%rdi
 1c5:	0f 87 c6 00 00 00    	ja     0x291
 1cb:	c4 a1 12 59 04 33    	vmulss (%rbx,%r14,1),%xmm13,%xmm0
 1d1:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
 1d6:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 1da:	4c 39 ff             	cmp    %r15,%rdi
 1dd:	0f 87 ae 00 00 00    	ja     0x291
 1e3:	f3 46 0f 11 24 2b    	movss  %xmm12,(%rbx,%r13,1)
 1e9:	41 83 c5 04          	add    $0x4,%r13d
 1ed:	41 83 c6 04          	add    $0x4,%r14d
 1f1:	41 83 ec 01          	sub    $0x1,%r12d
 1f5:	0f 85 c3 ff ff ff    	jne    0x1be
 1fb:	e9 4b 00 00 00       	jmp    0x24b
 200:	45 85 e4             	test   %r12d,%r12d
 203:	0f 84 42 00 00 00    	je     0x24b
 209:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 20e:	49 8d 7e 04          	lea    0x4(%r14),%rdi
 212:	4c 39 ff             	cmp    %r15,%rdi
 215:	0f 87 76 00 00 00    	ja     0x291
 21b:	c4 a1 12 59 04 33    	vmulss (%rbx,%r14,1),%xmm13,%xmm0
 221:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
 226:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 22a:	4c 39 ff             	cmp    %r15,%rdi
 22d:	0f 87 5e 00 00 00    	ja     0x291
 233:	f3 46 0f 11 24 2b    	movss  %xmm12,(%rbx,%r13,1)
 239:	41 83 c5 04          	add    $0x4,%r13d
 23d:	41 83 c6 04          	add    $0x4,%r14d
 241:	41 83 ec 01          	sub    $0x1,%r12d
 245:	0f 85 c3 ff ff ff    	jne    0x20e
 24b:	4c 89 6c 24 48       	mov    %r13,0x48(%rsp)
 250:	4c 89 74 24 50       	mov    %r14,0x50(%rsp)
 255:	4c 89 64 24 58       	mov    %r12,0x58(%rsp)
 25a:	f2 44 0f 11 64 24 60 	movsd  %xmm12,0x60(%rsp)
 261:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 266:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 26b:	48 89 07             	mov    %rax,(%rdi)
 26e:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 273:	48 89 47 08          	mov    %rax,0x8(%rdi)
 277:	48 8b 44 24 58       	mov    0x58(%rsp),%rax
 27c:	48 89 47 10          	mov    %rax,0x10(%rdi)
 280:	48 8b 44 24 60       	mov    0x60(%rsp),%rax
 285:	48 89 47 18          	mov    %rax,0x18(%rdi)
 289:	48 81 c4 78 00 00 00 	add    $0x78,%rsp
 290:	c3                   	ret
 291:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 296:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 29a:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 2a1:	89 46 14             	mov    %eax,0x14(%rsi)
 2a4:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 2aa:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 2ae:	c3                   	ret

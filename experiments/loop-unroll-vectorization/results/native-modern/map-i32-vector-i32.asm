
experiments/loop-unroll-vectorization/results/native-modern/map-i32-vector-i32.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	4c 8b 47 18          	mov    0x18(%rdi),%r8
  1a:	4c 8b 4f 20          	mov    0x20(%rdi),%r9
  1e:	e8 1d 00 00 00       	call   0x40
  23:	5f                   	pop    %rdi
  24:	48 89 07             	mov    %rax,(%rdi)
  27:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  2b:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2f:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  33:	c3                   	ret
  34:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3b:	00 00 
  3d:	0f 1f 00             	nopl   (%rax)
  40:	48 81 ec 58 00 00 00 	sub    $0x58,%rsp
  47:	41 89 c5             	mov    %eax,%r13d
  4a:	41 89 ce             	mov    %ecx,%r14d
  4d:	41 89 d4             	mov    %edx,%r12d
  50:	45 89 c3             	mov    %r8d,%r11d
  53:	44 89 cd             	mov    %r9d,%ebp
  56:	31 c0                	xor    %eax,%eax
  58:	45 31 d2             	xor    %r10d,%r10d
  5b:	66 45 0f 57 e4       	xorpd  %xmm12,%xmm12
  60:	8b 7b fc             	mov    -0x4(%rbx),%edi
  63:	8b 73 fc             	mov    -0x4(%rbx),%esi
  66:	44 89 e0             	mov    %r12d,%eax
  69:	48 c1 e0 02          	shl    $0x2,%rax
  6d:	49 89 c0             	mov    %rax,%r8
  70:	44 89 f0             	mov    %r14d,%eax
  73:	4c 01 c0             	add    %r8,%rax
  76:	89 ff                	mov    %edi,%edi
  78:	48 c1 e7 10          	shl    $0x10,%rdi
  7c:	48 39 f8             	cmp    %rdi,%rax
  7f:	41 0f 96 c1          	setbe  %r9b
  83:	45 0f b6 c9          	movzbl %r9b,%r9d
  87:	44 89 e7             	mov    %r12d,%edi
  8a:	48 c1 e7 02          	shl    $0x2,%rdi
  8e:	44 89 f0             	mov    %r14d,%eax
  91:	48 01 f8             	add    %rdi,%rax
  94:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  9b:	00 00 00 
  9e:	48 39 f8             	cmp    %rdi,%rax
  a1:	0f 96 c0             	setbe  %al
  a4:	0f b6 c0             	movzbl %al,%eax
  a7:	41 21 c1             	and    %eax,%r9d
  aa:	44 89 e7             	mov    %r12d,%edi
  ad:	48 c1 e7 02          	shl    $0x2,%rdi
  b1:	44 89 e8             	mov    %r13d,%eax
  b4:	48 01 f8             	add    %rdi,%rax
  b7:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  be:	00 00 00 
  c1:	48 39 f8             	cmp    %rdi,%rax
  c4:	0f 96 c0             	setbe  %al
  c7:	0f b6 c0             	movzbl %al,%eax
  ca:	41 21 c1             	and    %eax,%r9d
  cd:	44 89 e7             	mov    %r12d,%edi
  d0:	48 c1 e7 02          	shl    $0x2,%rdi
  d4:	44 89 e8             	mov    %r13d,%eax
  d7:	48 01 f8             	add    %rdi,%rax
  da:	89 f6                	mov    %esi,%esi
  dc:	48 c1 e6 10          	shl    $0x10,%rsi
  e0:	48 39 f0             	cmp    %rsi,%rax
  e3:	0f 96 c0             	setbe  %al
  e6:	0f b6 c0             	movzbl %al,%eax
  e9:	41 21 c1             	and    %eax,%r9d
  ec:	44 89 f6             	mov    %r14d,%esi
  ef:	44 39 ee             	cmp    %r13d,%esi
  f2:	40 0f 94 c7          	sete   %dil
  f6:	40 0f b6 ff          	movzbl %dil,%edi
  fa:	44 89 e6             	mov    %r12d,%esi
  fd:	48 c1 e6 02          	shl    $0x2,%rsi
 101:	44 89 f0             	mov    %r14d,%eax
 104:	48 01 f0             	add    %rsi,%rax
 107:	44 89 ee             	mov    %r13d,%esi
 10a:	48 39 f0             	cmp    %rsi,%rax
 10d:	0f 96 c0             	setbe  %al
 110:	0f b6 c0             	movzbl %al,%eax
 113:	09 c7                	or     %eax,%edi
 115:	44 89 e6             	mov    %r12d,%esi
 118:	48 c1 e6 02          	shl    $0x2,%rsi
 11c:	44 89 e8             	mov    %r13d,%eax
 11f:	48 01 f0             	add    %rsi,%rax
 122:	44 89 f6             	mov    %r14d,%esi
 125:	48 39 f0             	cmp    %rsi,%rax
 128:	0f 96 c0             	setbe  %al
 12b:	0f b6 c0             	movzbl %al,%eax
 12e:	09 c7                	or     %eax,%edi
 130:	41 21 f9             	and    %edi,%r9d
 133:	45 85 c9             	test   %r9d,%r9d
 136:	0f 84 e2 00 00 00    	je     0x21e
 13c:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 143:	00 00 
 145:	0f 1f 00             	nopl   (%rax)
 148:	41 83 fc 04          	cmp    $0x4,%r12d
 14c:	0f 82 7b 00 00 00    	jb     0x1cd
 152:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 157:	45 89 f6             	mov    %r14d,%r14d
 15a:	49 8d 7e 10          	lea    0x10(%r14),%rdi
 15e:	4c 39 ff             	cmp    %r15,%rdi
 161:	0f 87 3a 01 00 00    	ja     0x2a1
 167:	c4 a1 7a 6f 04 33    	vmovdqu (%rbx,%r14,1),%xmm0
 16d:	44 89 df             	mov    %r11d,%edi
 170:	66 0f 6e cf          	movd   %edi,%xmm1
 174:	66 0f 70 c9 00       	pshufd $0x0,%xmm1,%xmm1
 179:	c4 e2 79 40 c1       	vpmulld %xmm1,%xmm0,%xmm0
 17e:	89 ef                	mov    %ebp,%edi
 180:	66 0f 6e cf          	movd   %edi,%xmm1
 184:	66 0f 70 c9 00       	pshufd $0x0,%xmm1,%xmm1
 189:	c4 e1 79 fe c1       	vpaddd %xmm1,%xmm0,%xmm0
 18e:	c4 61 7a 6f e0       	vmovdqu %xmm0,%xmm12
 193:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
 198:	45 89 ed             	mov    %r13d,%r13d
 19b:	49 8d 7d 10          	lea    0x10(%r13),%rdi
 19f:	4c 39 ff             	cmp    %r15,%rdi
 1a2:	0f 87 f9 00 00 00    	ja     0x2a1
 1a8:	c4 a1 7a 7f 04 2b    	vmovdqu %xmm0,(%rbx,%r13,1)
 1ae:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
 1b3:	66 0f 3a 16 c7 03    	pextrd $0x3,%xmm0,%edi
 1b9:	41 89 fa             	mov    %edi,%r10d
 1bc:	41 83 c5 10          	add    $0x10,%r13d
 1c0:	41 83 c6 10          	add    $0x10,%r14d
 1c4:	41 83 ec 04          	sub    $0x4,%r12d
 1c8:	e9 7b ff ff ff       	jmp    0x148
 1cd:	0f 1f 00             	nopl   (%rax)
 1d0:	45 85 e4             	test   %r12d,%r12d
 1d3:	0f 84 40 00 00 00    	je     0x219
 1d9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 1de:	49 8d 7e 04          	lea    0x4(%r14),%rdi
 1e2:	4c 39 ff             	cmp    %r15,%rdi
 1e5:	0f 87 b6 00 00 00    	ja     0x2a1
 1eb:	46 8b 14 33          	mov    (%rbx,%r14,1),%r10d
 1ef:	45 0f af d3          	imul   %r11d,%r10d
 1f3:	41 01 ea             	add    %ebp,%r10d
 1f6:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 1fa:	4c 39 ff             	cmp    %r15,%rdi
 1fd:	0f 87 9e 00 00 00    	ja     0x2a1
 203:	46 89 14 2b          	mov    %r10d,(%rbx,%r13,1)
 207:	41 83 c5 04          	add    $0x4,%r13d
 20b:	41 83 c6 04          	add    $0x4,%r14d
 20f:	41 83 ec 01          	sub    $0x1,%r12d
 213:	0f 85 c5 ff ff ff    	jne    0x1de
 219:	e9 53 00 00 00       	jmp    0x271
 21e:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 225:	00 00 
 227:	90                   	nop
 228:	45 85 e4             	test   %r12d,%r12d
 22b:	0f 84 40 00 00 00    	je     0x271
 231:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 236:	49 8d 7e 04          	lea    0x4(%r14),%rdi
 23a:	4c 39 ff             	cmp    %r15,%rdi
 23d:	0f 87 5e 00 00 00    	ja     0x2a1
 243:	46 8b 14 33          	mov    (%rbx,%r14,1),%r10d
 247:	45 0f af d3          	imul   %r11d,%r10d
 24b:	41 01 ea             	add    %ebp,%r10d
 24e:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 252:	4c 39 ff             	cmp    %r15,%rdi
 255:	0f 87 46 00 00 00    	ja     0x2a1
 25b:	46 89 14 2b          	mov    %r10d,(%rbx,%r13,1)
 25f:	41 83 c5 04          	add    $0x4,%r13d
 263:	41 83 c6 04          	add    $0x4,%r14d
 267:	41 83 ec 01          	sub    $0x1,%r12d
 26b:	0f 85 c5 ff ff ff    	jne    0x236
 271:	4c 89 6c 24 28       	mov    %r13,0x28(%rsp)
 276:	4c 89 74 24 30       	mov    %r14,0x30(%rsp)
 27b:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 280:	4c 89 54 24 40       	mov    %r10,0x40(%rsp)
 285:	48 8b 44 24 28       	mov    0x28(%rsp),%rax
 28a:	48 8b 54 24 30       	mov    0x30(%rsp),%rdx
 28f:	48 8b 4c 24 38       	mov    0x38(%rsp),%rcx
 294:	4c 8b 44 24 40       	mov    0x40(%rsp),%r8
 299:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
 2a0:	c3                   	ret
 2a1:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 2a6:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 2aa:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 2b1:	89 46 14             	mov    %eax,0x14(%rsi)
 2b4:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 2ba:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 2be:	c3                   	ret

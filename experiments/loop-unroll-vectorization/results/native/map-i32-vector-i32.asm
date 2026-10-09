
experiments/loop-unroll-vectorization/results/native/map-i32-vector-i32.bin:     file format binary


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
  40:	48 81 ec 88 00 00 00 	sub    $0x88,%rsp
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
 136:	0f 84 62 01 00 00    	je     0x29e
 13c:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 143:	00 00 
 145:	0f 1f 00             	nopl   (%rax)
 148:	41 83 fc 04          	cmp    $0x4,%r12d
 14c:	0f 82 f0 00 00 00    	jb     0x242
 152:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 157:	45 89 f6             	mov    %r14d,%r14d
 15a:	49 8d 7e 10          	lea    0x10(%r14),%rdi
 15e:	4c 39 ff             	cmp    %r15,%rdi
 161:	0f 87 ba 01 00 00    	ja     0x321
 167:	f3 42 0f 6f 04 33    	movdqu (%rbx,%r14,1),%xmm0
 16d:	44 89 df             	mov    %r11d,%edi
 170:	66 0f 6e cf          	movd   %edi,%xmm1
 174:	66 0f 70 c9 00       	pshufd $0x0,%xmm1,%xmm1
 179:	48 89 44 24 60       	mov    %rax,0x60(%rsp)
 17e:	48 89 54 24 68       	mov    %rdx,0x68(%rsp)
 183:	48 89 4c 24 70       	mov    %rcx,0x70(%rsp)
 188:	f3 0f 7f 44 24 28    	movdqu %xmm0,0x28(%rsp)
 18e:	f3 0f 7f 4c 24 38    	movdqu %xmm1,0x38(%rsp)
 194:	8b 44 24 28          	mov    0x28(%rsp),%eax
 198:	8b 54 24 38          	mov    0x38(%rsp),%edx
 19c:	0f af c2             	imul   %edx,%eax
 19f:	89 44 24 48          	mov    %eax,0x48(%rsp)
 1a3:	8b 44 24 2c          	mov    0x2c(%rsp),%eax
 1a7:	8b 54 24 3c          	mov    0x3c(%rsp),%edx
 1ab:	0f af c2             	imul   %edx,%eax
 1ae:	89 44 24 4c          	mov    %eax,0x4c(%rsp)
 1b2:	8b 44 24 30          	mov    0x30(%rsp),%eax
 1b6:	8b 54 24 40          	mov    0x40(%rsp),%edx
 1ba:	0f af c2             	imul   %edx,%eax
 1bd:	89 44 24 50          	mov    %eax,0x50(%rsp)
 1c1:	8b 44 24 34          	mov    0x34(%rsp),%eax
 1c5:	8b 54 24 44          	mov    0x44(%rsp),%edx
 1c9:	0f af c2             	imul   %edx,%eax
 1cc:	89 44 24 54          	mov    %eax,0x54(%rsp)
 1d0:	f3 0f 6f 44 24 48    	movdqu 0x48(%rsp),%xmm0
 1d6:	48 8b 44 24 60       	mov    0x60(%rsp),%rax
 1db:	48 8b 54 24 68       	mov    0x68(%rsp),%rdx
 1e0:	48 8b 4c 24 70       	mov    0x70(%rsp),%rcx
 1e5:	89 ef                	mov    %ebp,%edi
 1e7:	66 0f 6e cf          	movd   %edi,%xmm1
 1eb:	66 0f 70 c9 00       	pshufd $0x0,%xmm1,%xmm1
 1f0:	66 0f fe c1          	paddd  %xmm1,%xmm0
 1f4:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
 1f9:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
 1fe:	45 89 ed             	mov    %r13d,%r13d
 201:	49 8d 7d 10          	lea    0x10(%r13),%rdi
 205:	4c 39 ff             	cmp    %r15,%rdi
 208:	0f 87 13 01 00 00    	ja     0x321
 20e:	f3 42 0f 7f 04 2b    	movdqu %xmm0,(%rbx,%r13,1)
 214:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
 219:	f3 0f 7f 4c 24 28    	movdqu %xmm1,0x28(%rsp)
 21f:	66 0f 70 c8 03       	pshufd $0x3,%xmm0,%xmm1
 224:	66 0f 7e cf          	movd   %xmm1,%edi
 228:	f3 0f 6f 4c 24 28    	movdqu 0x28(%rsp),%xmm1
 22e:	41 89 fa             	mov    %edi,%r10d
 231:	41 83 c5 10          	add    $0x10,%r13d
 235:	41 83 c6 10          	add    $0x10,%r14d
 239:	41 83 ec 04          	sub    $0x4,%r12d
 23d:	e9 06 ff ff ff       	jmp    0x148
 242:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 249:	00 00 
 24b:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 250:	45 85 e4             	test   %r12d,%r12d
 253:	0f 84 40 00 00 00    	je     0x299
 259:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 25e:	49 8d 7e 04          	lea    0x4(%r14),%rdi
 262:	4c 39 ff             	cmp    %r15,%rdi
 265:	0f 87 b6 00 00 00    	ja     0x321
 26b:	46 8b 14 33          	mov    (%rbx,%r14,1),%r10d
 26f:	45 0f af d3          	imul   %r11d,%r10d
 273:	41 01 ea             	add    %ebp,%r10d
 276:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 27a:	4c 39 ff             	cmp    %r15,%rdi
 27d:	0f 87 9e 00 00 00    	ja     0x321
 283:	46 89 14 2b          	mov    %r10d,(%rbx,%r13,1)
 287:	41 83 c5 04          	add    $0x4,%r13d
 28b:	41 83 c6 04          	add    $0x4,%r14d
 28f:	41 83 ec 01          	sub    $0x1,%r12d
 293:	0f 85 c5 ff ff ff    	jne    0x25e
 299:	e9 53 00 00 00       	jmp    0x2f1
 29e:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 2a5:	00 00 
 2a7:	90                   	nop
 2a8:	45 85 e4             	test   %r12d,%r12d
 2ab:	0f 84 40 00 00 00    	je     0x2f1
 2b1:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 2b6:	49 8d 7e 04          	lea    0x4(%r14),%rdi
 2ba:	4c 39 ff             	cmp    %r15,%rdi
 2bd:	0f 87 5e 00 00 00    	ja     0x321
 2c3:	46 8b 14 33          	mov    (%rbx,%r14,1),%r10d
 2c7:	45 0f af d3          	imul   %r11d,%r10d
 2cb:	41 01 ea             	add    %ebp,%r10d
 2ce:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 2d2:	4c 39 ff             	cmp    %r15,%rdi
 2d5:	0f 87 46 00 00 00    	ja     0x321
 2db:	46 89 14 2b          	mov    %r10d,(%rbx,%r13,1)
 2df:	41 83 c5 04          	add    $0x4,%r13d
 2e3:	41 83 c6 04          	add    $0x4,%r14d
 2e7:	41 83 ec 01          	sub    $0x1,%r12d
 2eb:	0f 85 c5 ff ff ff    	jne    0x2b6
 2f1:	4c 89 6c 24 28       	mov    %r13,0x28(%rsp)
 2f6:	4c 89 74 24 30       	mov    %r14,0x30(%rsp)
 2fb:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 300:	4c 89 54 24 40       	mov    %r10,0x40(%rsp)
 305:	48 8b 44 24 28       	mov    0x28(%rsp),%rax
 30a:	48 8b 54 24 30       	mov    0x30(%rsp),%rdx
 30f:	48 8b 4c 24 38       	mov    0x38(%rsp),%rcx
 314:	4c 8b 44 24 40       	mov    0x40(%rsp),%r8
 319:	48 81 c4 88 00 00 00 	add    $0x88,%rsp
 320:	c3                   	ret
 321:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 326:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 32a:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 331:	89 46 14             	mov    %eax,0x14(%rsi)
 334:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 33a:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 33e:	c3                   	ret

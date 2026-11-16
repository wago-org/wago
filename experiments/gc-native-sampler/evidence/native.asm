
native.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	51                   	push   %rcx
   4:	e8 05 00 00 00       	call   0xe
   9:	59                   	pop    %rcx
   a:	48 89 01             	mov    %rax,(%rcx)
   d:	c3                   	ret
   e:	48 81 ec 18 00 00 00 	sub    $0x18,%rsp
  15:	48 8b 73 b8          	mov    -0x48(%rbx),%rsi
  19:	48 39 f4             	cmp    %rsi,%rsp
  1c:	0f 82 43 00 00 00    	jb     0x65
  22:	48 8b 7b 90          	mov    -0x70(%rbx),%rdi
  26:	48 8b 3f             	mov    (%rdi),%rdi
  29:	48 8b 37             	mov    (%rdi),%rsi
  2c:	48 89 34 24          	mov    %rsi,(%rsp)
  30:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
  34:	48 8b 04 24          	mov    (%rsp),%rax
  38:	49 89 40 48          	mov    %rax,0x48(%r8)
  3c:	41 c7 40 40 15 00 00 	movl   $0x40000015,0x40(%r8)
  43:	40 
  44:	41 c7 40 44 01 00 01 	movl   $0x10001,0x44(%r8)
  4b:	00 
  4c:	41 ff 50 38          	call   *0x38(%r8)
  50:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
  54:	49 8b b8 48 02 00 00 	mov    0x248(%r8),%rdi
  5b:	89 f8                	mov    %edi,%eax
  5d:	48 81 c4 18 00 00 00 	add    $0x18,%rsp
  64:	c3                   	ret
  65:	b8 00 00 00 00       	mov    $0x0,%eax
  6a:	e9 00 00 00 00       	jmp    0x6f
  6f:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  73:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  7a:	89 46 14             	mov    %eax,0x14(%rsi)
  7d:	c7 06 0d 00 00 00    	movl   $0xd,(%rsi)
  83:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  87:	c3                   	ret
	...
  90:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  97:	48 89 f3             	mov    %rsi,%rbx
  9a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
  9f:	48 8b 43 b8          	mov    -0x48(%rbx),%rax
  a3:	48 39 c4             	cmp    %rax,%rsp
  a6:	0f 82 7c 00 00 00    	jb     0x128
  ac:	44 8b 27             	mov    (%rdi),%r12d
  af:	48 8b 7b 90          	mov    -0x70(%rbx),%rdi
  b3:	48 8b 3f             	mov    (%rdi),%rdi
  b6:	48 8b 37             	mov    (%rdi),%rsi
  b9:	48 89 74 24 18       	mov    %rsi,0x18(%rsp)
  be:	4c 89 64 24 20       	mov    %r12,0x20(%rsp)
  c3:	bf 01 00 00 00       	mov    $0x1,%edi
  c8:	48 89 7c 24 28       	mov    %rdi,0x28(%rsp)
  cd:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
  d1:	48 8b 44 24 18       	mov    0x18(%rsp),%rax
  d6:	49 89 40 48          	mov    %rax,0x48(%r8)
  da:	8b 44 24 20          	mov    0x20(%rsp),%eax
  de:	49 89 40 50          	mov    %rax,0x50(%r8)
  e2:	8b 44 24 28          	mov    0x28(%rsp),%eax
  e6:	49 89 40 58          	mov    %rax,0x58(%r8)
  ea:	44 89 64 24 10       	mov    %r12d,0x10(%rsp)
  ef:	41 c7 40 40 11 00 00 	movl   $0x40000011,0x40(%r8)
  f6:	40 
  f7:	41 c7 40 44 03 00 01 	movl   $0x10003,0x44(%r8)
  fe:	00 
  ff:	41 ff 50 38          	call   *0x38(%r8)
 103:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 107:	49 8b b8 48 02 00 00 	mov    0x248(%r8),%rdi
 10e:	48 89 7c 24 18       	mov    %rdi,0x18(%rsp)
 113:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 118:	48 8b 44 24 18       	mov    0x18(%rsp),%rax
 11d:	48 89 07             	mov    %rax,(%rdi)
 120:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 127:	c3                   	ret
 128:	b8 00 00 00 00       	mov    $0x0,%eax
 12d:	e9 00 00 00 00       	jmp    0x132
 132:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 136:	c7 46 10 02 00 00 00 	movl   $0x2,0x10(%rsi)
 13d:	89 46 14             	mov    %eax,0x14(%rsi)
 140:	c7 06 0d 00 00 00    	movl   $0xd,(%rsi)
 146:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 14a:	c3                   	ret
 14b:	00 00                	add    %al,(%rax)
 14d:	00 00                	add    %al,(%rax)
 14f:	00 48 89             	add    %cl,-0x77(%rax)
 152:	f3 51                	repz push %rcx
 154:	48 8b 07             	mov    (%rdi),%rax
 157:	e8 05 00 00 00       	call   0x161
 15c:	59                   	pop    %rcx
 15d:	48 89 01             	mov    %rax,(%rcx)
 160:	c3                   	ret
 161:	48 81 ec 28 00 00 00 	sub    $0x28,%rsp
 168:	48 8b 73 b8          	mov    -0x48(%rbx),%rsi
 16c:	48 39 f4             	cmp    %rsi,%rsp
 16f:	0f 82 94 00 00 00    	jb     0x209
 175:	41 89 c4             	mov    %eax,%r12d
 178:	48 8b 7b 90          	mov    -0x70(%rbx),%rdi
 17c:	48 8b 3f             	mov    (%rdi),%rdi
 17f:	48 8b 37             	mov    (%rdi),%rsi
 182:	48 89 74 24 08       	mov    %rsi,0x8(%rsp)
 187:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
 18c:	bf 01 00 00 00       	mov    $0x1,%edi
 191:	48 89 7c 24 18       	mov    %rdi,0x18(%rsp)
 196:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 19a:	48 8b 44 24 08       	mov    0x8(%rsp),%rax
 19f:	49 89 40 48          	mov    %rax,0x48(%r8)
 1a3:	8b 44 24 10          	mov    0x10(%rsp),%eax
 1a7:	49 89 40 50          	mov    %rax,0x50(%r8)
 1ab:	8b 44 24 18          	mov    0x18(%rsp),%eax
 1af:	49 89 40 58          	mov    %rax,0x58(%r8)
 1b3:	44 89 24 24          	mov    %r12d,(%rsp)
 1b7:	41 c7 40 40 11 00 00 	movl   $0x40000011,0x40(%r8)
 1be:	40 
 1bf:	41 c7 40 44 03 00 01 	movl   $0x10003,0x44(%r8)
 1c6:	00 
 1c7:	41 ff 50 38          	call   *0x38(%r8)
 1cb:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 1cf:	49 8b b8 48 02 00 00 	mov    0x248(%r8),%rdi
 1d6:	48 89 7c 24 08       	mov    %rdi,0x8(%rsp)
 1db:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 1e0:	85 ff                	test   %edi,%edi
 1e2:	0f 84 44 00 00 00    	je     0x22c
 1e8:	89 ff                	mov    %edi,%edi
 1ea:	48 8b b3 e8 fe ff ff 	mov    -0x118(%rbx),%rsi
 1f1:	48 8b 76 08          	mov    0x8(%rsi),%rsi
 1f5:	48 8b 76 08          	mov    0x8(%rsi),%rsi
 1f9:	48 01 fe             	add    %rdi,%rsi
 1fc:	8b 7e 10             	mov    0x10(%rsi),%edi
 1ff:	89 f8                	mov    %edi,%eax
 201:	48 81 c4 28 00 00 00 	add    $0x28,%rsp
 208:	c3                   	ret
 209:	b8 00 00 00 00       	mov    $0x0,%eax
 20e:	e9 00 00 00 00       	jmp    0x213
 213:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 217:	c7 46 10 03 00 00 00 	movl   $0x3,0x10(%rsi)
 21e:	89 46 14             	mov    %eax,0x14(%rsi)
 221:	c7 06 0d 00 00 00    	movl   $0xd,(%rsi)
 227:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 22b:	c3                   	ret
 22c:	b8 08 00 00 00       	mov    $0x8,%eax
 231:	e9 00 00 00 00       	jmp    0x236
 236:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 23a:	c7 46 10 03 00 00 00 	movl   $0x3,0x10(%rsi)
 241:	89 46 14             	mov    %eax,0x14(%rsi)
 244:	c7 06 10 00 00 00    	movl   $0x10,(%rsi)
 24a:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 24e:	c3                   	ret
	...
 27f:	00 48 89             	add    %cl,-0x77(%rax)
 282:	f3 51                	repz push %rcx
 284:	48 8b 07             	mov    (%rdi),%rax
 287:	e8 05 00 00 00       	call   0x291
 28c:	59                   	pop    %rcx
 28d:	48 89 01             	mov    %rax,(%rcx)
 290:	c3                   	ret
 291:	48 81 ec 48 00 00 00 	sub    $0x48,%rsp
 298:	48 8b 73 b8          	mov    -0x48(%rbx),%rsi
 29c:	48 39 f4             	cmp    %rsi,%rsp
 29f:	0f 82 32 03 00 00    	jb     0x5d7
 2a5:	41 89 c7             	mov    %eax,%r15d
 2a8:	31 c0                	xor    %eax,%eax
 2aa:	45 31 e4             	xor    %r12d,%r12d
 2ad:	45 31 f6             	xor    %r14d,%r14d
 2b0:	45 31 ed             	xor    %r13d,%r13d
 2b3:	4c 89 7c 24 20       	mov    %r15,0x20(%rsp)
 2b8:	bf 01 00 00 00       	mov    $0x1,%edi
 2bd:	48 89 7c 24 28       	mov    %rdi,0x28(%rsp)
 2c2:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 2c6:	8b 44 24 20          	mov    0x20(%rsp),%eax
 2ca:	49 89 40 48          	mov    %rax,0x48(%r8)
 2ce:	8b 44 24 28          	mov    0x28(%rsp),%eax
 2d2:	49 89 40 50          	mov    %rax,0x50(%r8)
 2d6:	44 89 3c 24          	mov    %r15d,(%rsp)
 2da:	44 89 64 24 08       	mov    %r12d,0x8(%rsp)
 2df:	44 89 74 24 10       	mov    %r14d,0x10(%rsp)
 2e4:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
 2e9:	41 c7 40 40 10 01 00 	movl   $0x40000110,0x40(%r8)
 2f0:	40 
 2f1:	41 c7 40 44 02 00 01 	movl   $0x10002,0x44(%r8)
 2f8:	00 
 2f9:	41 ff 50 38          	call   *0x38(%r8)
 2fd:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 301:	49 8b b8 48 02 00 00 	mov    0x248(%r8),%rdi
 308:	48 8b 73 90          	mov    -0x70(%rbx),%rsi
 30c:	48 8b 36             	mov    (%rsi),%rsi
 30f:	48 89 3e             	mov    %rdi,(%rsi)
 312:	44 8b 3c 24          	mov    (%rsp),%r15d
 316:	45 85 ff             	test   %r15d,%r15d
 319:	0f 84 a8 02 00 00    	je     0x5c7
 31f:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 324:	44 8b 64 24 08       	mov    0x8(%rsp),%r12d
 329:	44 8b 74 24 10       	mov    0x10(%rsp),%r14d
 32e:	4c 8b 6c 24 18       	mov    0x18(%rsp),%r13
 333:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 33a:	00 00 
 33c:	0f 1f 40 00          	nopl   0x0(%rax)
 340:	4c 89 64 24 20       	mov    %r12,0x20(%rsp)
 345:	bf 00 00 00 00       	mov    $0x0,%edi
 34a:	48 89 7c 24 28       	mov    %rdi,0x28(%rsp)
 34f:	41 50                	push   %r8
 351:	4c 8b 83 e8 fe ff ff 	mov    -0x118(%rbx),%r8
 358:	49 8b 40 10          	mov    0x10(%r8),%rax
 35c:	8b 10                	mov    (%rax),%edx
 35e:	4d 8b 40 08          	mov    0x8(%r8),%r8
 362:	41 8b 40 44          	mov    0x44(%r8),%eax
 366:	83 f8 01             	cmp    $0x1,%eax
 369:	0f 85 8f 00 00 00    	jne    0x3fe
 36f:	4d 8b 48 38          	mov    0x38(%r8),%r9
 373:	45 8b 11             	mov    (%r9),%r10d
 376:	45 3b 50 2c          	cmp    0x2c(%r8),%r10d
 37a:	0f 85 7e 00 00 00    	jne    0x3fe
 380:	41 8b 48 40          	mov    0x40(%r8),%ecx
 384:	41 39 ca             	cmp    %ecx,%r10d
 387:	0f 87 71 00 00 00    	ja     0x3fe
 38d:	44 29 d1             	sub    %r10d,%ecx
 390:	83 f9 20             	cmp    $0x20,%ecx
 393:	0f 82 65 00 00 00    	jb     0x3fe
 399:	4d 8b 58 08          	mov    0x8(%r8),%r11
 39d:	4d 01 d3             	add    %r10,%r11
 3a0:	44 89 d0             	mov    %r10d,%eax
 3a3:	c1 e8 03             	shr    $0x3,%eax
 3a6:	49 8b 70 18          	mov    0x18(%r8),%rsi
 3aa:	48 01 c6             	add    %rax,%rsi
 3ad:	44 89 d1             	mov    %r10d,%ecx
 3b0:	83 c1 20             	add    $0x20,%ecx
 3b3:	41 89 13             	mov    %edx,(%r11)
 3b6:	41 c7 43 04 18 00 00 	movl   $0x18,0x4(%r11)
 3bd:	00 
 3be:	41 c7 43 08 00 00 00 	movl   $0x0,0x8(%r11)
 3c5:	00 
 3c6:	41 c7 43 0c 01 00 00 	movl   $0x1,0xc(%r11)
 3cd:	00 
 3ce:	b8 00 00 00 00       	mov    $0x0,%eax
 3d3:	49 89 43 10          	mov    %rax,0x10(%r11)
 3d7:	49 89 43 18          	mov    %rax,0x18(%r11)
 3db:	8b 44 24 28          	mov    0x28(%rsp),%eax
 3df:	41 89 43 10          	mov    %eax,0x10(%r11)
 3e3:	b8 01 00 00 00       	mov    $0x1,%eax
 3e8:	88 06                	mov    %al,(%rsi)
 3ea:	41 89 09             	mov    %ecx,(%r9)
 3ed:	41 89 48 2c          	mov    %ecx,0x2c(%r8)
 3f1:	44 89 d0             	mov    %r10d,%eax
 3f4:	48 89 44 24 28       	mov    %rax,0x28(%rsp)
 3f9:	e9 05 00 00 00       	jmp    0x403
 3fe:	b8 00 00 00 00       	mov    $0x0,%eax
 403:	85 c0                	test   %eax,%eax
 405:	41 58                	pop    %r8
 407:	0f 85 4b 00 00 00    	jne    0x458
 40d:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 411:	8b 44 24 20          	mov    0x20(%rsp),%eax
 415:	49 89 40 48          	mov    %rax,0x48(%r8)
 419:	8b 44 24 28          	mov    0x28(%rsp),%eax
 41d:	49 89 40 50          	mov    %rax,0x50(%r8)
 421:	41 c7 40 40 0b 02 00 	movl   $0x4000020b,0x40(%r8)
 428:	40 
 429:	41 c7 40 44 02 00 01 	movl   $0x10002,0x44(%r8)
 430:	00 
 431:	41 ff 50 38          	call   *0x38(%r8)
 435:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 439:	49 8b 80 48 02 00 00 	mov    0x248(%r8),%rax
 440:	48 89 44 24 20       	mov    %rax,0x20(%rsp)
 445:	44 8b 3c 24          	mov    (%rsp),%r15d
 449:	44 8b 64 24 08       	mov    0x8(%rsp),%r12d
 44e:	44 8b 74 24 10       	mov    0x10(%rsp),%r14d
 453:	4c 8b 6c 24 18       	mov    0x18(%rsp),%r13
 458:	4c 8b 6c 24 20       	mov    0x20(%rsp),%r13
 45d:	48 8b 7b 90          	mov    -0x70(%rbx),%rdi
 461:	48 8b 3f             	mov    (%rdi),%rdi
 464:	48 8b 37             	mov    (%rdi),%rsi
 467:	48 89 74 24 20       	mov    %rsi,0x20(%rsp)
 46c:	4c 89 64 24 28       	mov    %r12,0x28(%rsp)
 471:	4c 89 6c 24 30       	mov    %r13,0x30(%rsp)
 476:	bf 01 00 00 00       	mov    $0x1,%edi
 47b:	48 89 7c 24 38       	mov    %rdi,0x38(%rsp)
 480:	41 50                	push   %r8
 482:	44 8b 54 24 28       	mov    0x28(%rsp),%r10d
 487:	45 85 d2             	test   %r10d,%r10d
 48a:	0f 84 7b 00 00 00    	je     0x50b
 490:	4c 8b 83 e8 fe ff ff 	mov    -0x118(%rbx),%r8
 497:	4d 8b 40 08          	mov    0x8(%r8),%r8
 49b:	4d 8b 48 08          	mov    0x8(%r8),%r9
 49f:	4d 01 d1             	add    %r10,%r9
 4a2:	8b 44 24 30          	mov    0x30(%rsp),%eax
 4a6:	41 3b 41 08          	cmp    0x8(%r9),%eax
 4aa:	0f 83 5b 00 00 00    	jae    0x50b
 4b0:	48 c1 e0 02          	shl    $0x2,%rax
 4b4:	49 8d 79 10          	lea    0x10(%r9),%rdi
 4b8:	48 01 c7             	add    %rax,%rdi
 4bb:	45 3b 50 28          	cmp    0x28(%r8),%r10d
 4bf:	0f 82 0a 00 00 00    	jb     0x4cf
 4c5:	45 3b 50 2c          	cmp    0x2c(%r8),%r10d
 4c9:	0f 82 2c 00 00 00    	jb     0x4fb
 4cf:	44 89 d0             	mov    %r10d,%eax
 4d2:	c1 e8 03             	shr    $0x3,%eax
 4d5:	41 3b 40 58          	cmp    0x58(%r8),%eax
 4d9:	0f 83 2c 00 00 00    	jae    0x50b
 4df:	49 8b 70 50          	mov    0x50(%r8),%rsi
 4e3:	48 85 f6             	test   %rsi,%rsi
 4e6:	0f 84 1f 00 00 00    	je     0x50b
 4ec:	48 01 c6             	add    %rax,%rsi
 4ef:	0f b6 06             	movzbl (%rsi),%eax
 4f2:	83 f8 01             	cmp    $0x1,%eax
 4f5:	0f 85 10 00 00 00    	jne    0x50b
 4fb:	8b 44 24 38          	mov    0x38(%rsp),%eax
 4ff:	89 07                	mov    %eax,(%rdi)
 501:	b8 01 00 00 00       	mov    $0x1,%eax
 506:	e9 05 00 00 00       	jmp    0x510
 50b:	b8 00 00 00 00       	mov    $0x0,%eax
 510:	85 c0                	test   %eax,%eax
 512:	41 58                	pop    %r8
 514:	0f 85 56 00 00 00    	jne    0x570
 51a:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 51e:	48 8b 44 24 20       	mov    0x20(%rsp),%rax
 523:	49 89 40 48          	mov    %rax,0x48(%r8)
 527:	8b 44 24 28          	mov    0x28(%rsp),%eax
 52b:	49 89 40 50          	mov    %rax,0x50(%r8)
 52f:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
 534:	49 89 40 58          	mov    %rax,0x58(%r8)
 538:	8b 44 24 38          	mov    0x38(%rsp),%eax
 53c:	49 89 40 60          	mov    %rax,0x60(%r8)
 540:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
 545:	41 c7 40 40 14 00 00 	movl   $0x40000014,0x40(%r8)
 54c:	40 
 54d:	41 c7 40 44 04 00 00 	movl   $0x4,0x44(%r8)
 554:	00 
 555:	41 ff 50 38          	call   *0x38(%r8)
 559:	4c 8b 43 d8          	mov    -0x28(%rbx),%r8
 55d:	44 8b 3c 24          	mov    (%rsp),%r15d
 561:	44 8b 64 24 08       	mov    0x8(%rsp),%r12d
 566:	44 8b 74 24 10       	mov    0x10(%rsp),%r14d
 56b:	4c 8b 6c 24 18       	mov    0x18(%rsp),%r13
 570:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
 575:	4c 89 6c 24 28       	mov    %r13,0x28(%rsp)
 57a:	48 8b 7c 24 28       	mov    0x28(%rsp),%rdi
 57f:	85 ff                	test   %edi,%edi
 581:	0f 84 73 00 00 00    	je     0x5fa
 587:	89 ff                	mov    %edi,%edi
 589:	48 8b b3 e8 fe ff ff 	mov    -0x118(%rbx),%rsi
 590:	48 8b 76 08          	mov    0x8(%rsi),%rsi
 594:	48 8b 76 08          	mov    0x8(%rsi),%rsi
 598:	48 01 fe             	add    %rdi,%rsi
 59b:	8b 7e 10             	mov    0x10(%rsi),%edi
 59e:	41 89 fe             	mov    %edi,%r14d
 5a1:	44 03 74 24 20       	add    0x20(%rsp),%r14d
 5a6:	41 83 c4 01          	add    $0x1,%r12d
 5aa:	44 89 64 24 08       	mov    %r12d,0x8(%rsp)
 5af:	44 89 74 24 10       	mov    %r14d,0x10(%rsp)
 5b4:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
 5b9:	45 39 fc             	cmp    %r15d,%r12d
 5bc:	0f 82 7e fd ff ff    	jb     0x340
 5c2:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 5c7:	44 8b 74 24 10       	mov    0x10(%rsp),%r14d
 5cc:	44 89 f0             	mov    %r14d,%eax
 5cf:	48 81 c4 48 00 00 00 	add    $0x48,%rsp
 5d6:	c3                   	ret
 5d7:	b8 00 00 00 00       	mov    $0x0,%eax
 5dc:	e9 00 00 00 00       	jmp    0x5e1
 5e1:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 5e5:	c7 46 10 04 00 00 00 	movl   $0x4,0x10(%rsi)
 5ec:	89 46 14             	mov    %eax,0x14(%rsi)
 5ef:	c7 06 0d 00 00 00    	movl   $0xd,(%rsi)
 5f5:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 5f9:	c3                   	ret
 5fa:	b8 2a 00 00 00       	mov    $0x2a,%eax
 5ff:	e9 00 00 00 00       	jmp    0x604
 604:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 608:	c7 46 10 04 00 00 00 	movl   $0x4,0x10(%rsi)
 60f:	89 46 14             	mov    %eax,0x14(%rsi)
 612:	c7 06 10 00 00 00    	movl   $0x10,(%rsi)
 618:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 61c:	c3                   	ret
	...
 63d:	00 00                	add    %al,(%rax)
 63f:	00 48 89             	add    %cl,-0x77(%rax)
 642:	f3 51                	repz push %rcx
 644:	48 8b 07             	mov    (%rdi),%rax
 647:	e8 05 00 00 00       	call   0x651
 64c:	59                   	pop    %rcx
 64d:	48 89 01             	mov    %rax,(%rcx)
 650:	c3                   	ret
 651:	48 81 ec 28 00 00 00 	sub    $0x28,%rsp
 658:	48 8b 73 b8          	mov    -0x48(%rbx),%rsi
 65c:	48 39 f4             	cmp    %rsi,%rsp
 65f:	0f 82 67 00 00 00    	jb     0x6cc
 665:	41 89 c6             	mov    %eax,%r14d
 668:	31 c0                	xor    %eax,%eax
 66a:	45 31 e4             	xor    %r12d,%r12d
 66d:	45 31 ed             	xor    %r13d,%r13d
 670:	44 89 34 24          	mov    %r14d,(%rsp)
 674:	44 89 64 24 08       	mov    %r12d,0x8(%rsp)
 679:	44 89 6c 24 10       	mov    %r13d,0x10(%rsp)
 67e:	66 90                	xchg   %ax,%ax
 680:	44 89 f7             	mov    %r14d,%edi
 683:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
 688:	48 89 f8             	mov    %rdi,%rax
 68b:	e8 01 fc ff ff       	call   0x291
 690:	48 89 c7             	mov    %rax,%rdi
 693:	41 89 fd             	mov    %edi,%r13d
 696:	44 03 6c 24 18       	add    0x18(%rsp),%r13d
 69b:	44 8b 64 24 08       	mov    0x8(%rsp),%r12d
 6a0:	41 83 c4 01          	add    $0x1,%r12d
 6a4:	44 89 64 24 08       	mov    %r12d,0x8(%rsp)
 6a9:	44 89 6c 24 10       	mov    %r13d,0x10(%rsp)
 6ae:	44 8b 34 24          	mov    (%rsp),%r14d
 6b2:	41 83 fc 40          	cmp    $0x40,%r12d
 6b6:	0f 82 c4 ff ff ff    	jb     0x680
 6bc:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 6c1:	44 89 e8             	mov    %r13d,%eax
 6c4:	48 81 c4 28 00 00 00 	add    $0x28,%rsp
 6cb:	c3                   	ret
 6cc:	b8 00 00 00 00       	mov    $0x0,%eax
 6d1:	e9 00 00 00 00       	jmp    0x6d6
 6d6:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 6da:	c7 46 10 05 00 00 00 	movl   $0x5,0x10(%rsi)
 6e1:	89 46 14             	mov    %eax,0x14(%rsi)
 6e4:	c7 06 0d 00 00 00    	movl   $0xd,(%rsi)
 6ea:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 6ee:	c3                   	ret

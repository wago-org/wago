
experiments/specialized-sum-unroll/results/followup/native-H/pressure12.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec a8 00 00 00 	sub    $0xa8,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	44 8b 27             	mov    (%rdi),%r12d
  19:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  1d:	4c 8b 77 10          	mov    0x10(%rdi),%r14
  21:	4c 8b 4f 18          	mov    0x18(%rdi),%r9
  25:	4c 8b 57 20          	mov    0x20(%rdi),%r10
  29:	4c 8b 5f 28          	mov    0x28(%rdi),%r11
  2d:	48 8b 6f 30          	mov    0x30(%rdi),%rbp
  31:	48 8b 47 38          	mov    0x38(%rdi),%rax
  35:	48 89 44 24 40       	mov    %rax,0x40(%rsp)
  3a:	48 8b 47 40          	mov    0x40(%rdi),%rax
  3e:	48 89 44 24 48       	mov    %rax,0x48(%rsp)
  43:	48 8b 47 48          	mov    0x48(%rdi),%rax
  47:	48 89 44 24 50       	mov    %rax,0x50(%rsp)
  4c:	48 8b 47 50          	mov    0x50(%rdi),%rax
  50:	48 89 44 24 58       	mov    %rax,0x58(%rsp)
  55:	48 8b 47 58          	mov    0x58(%rdi),%rax
  59:	48 89 44 24 60       	mov    %rax,0x60(%rsp)
  5e:	48 8b 47 60          	mov    0x60(%rdi),%rax
  62:	48 89 44 24 68       	mov    %rax,0x68(%rsp)
  67:	48 8b 47 68          	mov    0x68(%rdi),%rax
  6b:	48 89 44 24 70       	mov    %rax,0x70(%rsp)
  70:	48 8b 47 70          	mov    0x70(%rdi),%rax
  74:	48 89 44 24 78       	mov    %rax,0x78(%rsp)
  79:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  80:	00 00 
  82:	66 0f 1f 44 00 00    	nopw   0x0(%rax,%rax,1)
  88:	45 85 ed             	test   %r13d,%r13d
  8b:	0f 84 37 01 00 00    	je     0x1c8
  91:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  96:	44 89 ef             	mov    %r13d,%edi
  99:	48 c1 e7 03          	shl    $0x3,%rdi
  9d:	45 89 e4             	mov    %r12d,%r12d
  a0:	4c 01 e7             	add    %r12,%rdi
  a3:	4c 39 ff             	cmp    %r15,%rdi
  a6:	0f 86 20 00 00 00    	jbe    0xcc
  ac:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  b3:	00 00 00 
  b6:	4c 39 ff             	cmp    %r15,%rdi
  b9:	0f 85 9d 01 00 00    	jne    0x25c
  bf:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  c6:	0f 85 90 01 00 00    	jne    0x25c
  cc:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  d0:	41 83 c4 08          	add    $0x8,%r12d
  d4:	31 ff                	xor    %edi,%edi
  d6:	31 f6                	xor    %esi,%esi
  d8:	31 c0                	xor    %eax,%eax
  da:	41 83 ed 01          	sub    $0x1,%r13d
  de:	0f 84 db 00 00 00    	je     0x1bf
  e4:	41 83 fd 04          	cmp    $0x4,%r13d
  e8:	0f 82 b6 00 00 00    	jb     0x1a4
  ee:	44 89 ef             	mov    %r13d,%edi
  f1:	48 c1 e7 03          	shl    $0x3,%rdi
  f5:	4c 01 e7             	add    %r12,%rdi
  f8:	48 c1 ef 20          	shr    $0x20,%rdi
  fc:	bf 00 00 00 00       	mov    $0x0,%edi
 101:	0f 85 9d 00 00 00    	jne    0x1a4
 107:	41 83 fd 10          	cmp    $0x10,%r13d
 10b:	0f 82 6e 00 00 00    	jb     0x17f
 111:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 115:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
 11a:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
 11f:	4a 03 44 23 18       	add    0x18(%rbx,%r12,1),%rax
 124:	4e 03 74 23 20       	add    0x20(%rbx,%r12,1),%r14
 129:	4a 03 7c 23 28       	add    0x28(%rbx,%r12,1),%rdi
 12e:	4a 03 74 23 30       	add    0x30(%rbx,%r12,1),%rsi
 133:	4a 03 44 23 38       	add    0x38(%rbx,%r12,1),%rax
 138:	4e 03 74 23 40       	add    0x40(%rbx,%r12,1),%r14
 13d:	4a 03 7c 23 48       	add    0x48(%rbx,%r12,1),%rdi
 142:	4a 03 74 23 50       	add    0x50(%rbx,%r12,1),%rsi
 147:	4a 03 44 23 58       	add    0x58(%rbx,%r12,1),%rax
 14c:	4e 03 74 23 60       	add    0x60(%rbx,%r12,1),%r14
 151:	4a 03 7c 23 68       	add    0x68(%rbx,%r12,1),%rdi
 156:	4a 03 74 23 70       	add    0x70(%rbx,%r12,1),%rsi
 15b:	4a 03 44 23 78       	add    0x78(%rbx,%r12,1),%rax
 160:	41 81 c4 80 00 00 00 	add    $0x80,%r12d
 167:	41 83 ed 10          	sub    $0x10,%r13d
 16b:	41 83 fd 10          	cmp    $0x10,%r13d
 16f:	0f 83 9c ff ff ff    	jae    0x111
 175:	41 83 fd 04          	cmp    $0x4,%r13d
 179:	0f 82 25 00 00 00    	jb     0x1a4
 17f:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 183:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
 188:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
 18d:	4a 03 44 23 18       	add    0x18(%rbx,%r12,1),%rax
 192:	41 83 c4 20          	add    $0x20,%r12d
 196:	41 83 ed 04          	sub    $0x4,%r13d
 19a:	41 83 fd 04          	cmp    $0x4,%r13d
 19e:	0f 83 db ff ff ff    	jae    0x17f
 1a4:	45 85 ed             	test   %r13d,%r13d
 1a7:	0f 84 12 00 00 00    	je     0x1bf
 1ad:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 1b1:	41 83 c4 08          	add    $0x8,%r12d
 1b5:	41 83 ed 01          	sub    $0x1,%r13d
 1b9:	0f 85 ee ff ff ff    	jne    0x1ad
 1bf:	49 01 fe             	add    %rdi,%r14
 1c2:	49 01 f6             	add    %rsi,%r14
 1c5:	49 01 c6             	add    %rax,%r14
 1c8:	49 8d 39             	lea    (%r9),%rdi
 1cb:	49 8d 34 3a          	lea    (%r10,%rdi,1),%rsi
 1cf:	49 8d 3c 33          	lea    (%r11,%rsi,1),%rdi
 1d3:	48 8d 74 3d 00       	lea    0x0(%rbp,%rdi,1),%rsi
 1d8:	48 03 74 24 40       	add    0x40(%rsp),%rsi
 1dd:	48 03 74 24 48       	add    0x48(%rsp),%rsi
 1e2:	4c 89 b4 24 80 00 00 	mov    %r14,0x80(%rsp)
 1e9:	00 
 1ea:	4c 89 a4 24 88 00 00 	mov    %r12,0x88(%rsp)
 1f1:	00 
 1f2:	4c 89 ac 24 90 00 00 	mov    %r13,0x90(%rsp)
 1f9:	00 
 1fa:	48 03 74 24 50       	add    0x50(%rsp),%rsi
 1ff:	48 03 74 24 58       	add    0x58(%rsp),%rsi
 204:	48 03 74 24 60       	add    0x60(%rsp),%rsi
 209:	48 03 74 24 68       	add    0x68(%rsp),%rsi
 20e:	48 03 74 24 70       	add    0x70(%rsp),%rsi
 213:	48 03 74 24 78       	add    0x78(%rsp),%rsi
 218:	48 89 b4 24 98 00 00 	mov    %rsi,0x98(%rsp)
 21f:	00 
 220:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 225:	48 8b 84 24 80 00 00 	mov    0x80(%rsp),%rax
 22c:	00 
 22d:	48 89 07             	mov    %rax,(%rdi)
 230:	48 8b 84 24 88 00 00 	mov    0x88(%rsp),%rax
 237:	00 
 238:	48 89 47 08          	mov    %rax,0x8(%rdi)
 23c:	48 8b 84 24 90 00 00 	mov    0x90(%rsp),%rax
 243:	00 
 244:	48 89 47 10          	mov    %rax,0x10(%rdi)
 248:	48 8b 84 24 98 00 00 	mov    0x98(%rsp),%rax
 24f:	00 
 250:	48 89 47 18          	mov    %rax,0x18(%rdi)
 254:	48 81 c4 a8 00 00 00 	add    $0xa8,%rsp
 25b:	c3                   	ret
 25c:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 261:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 265:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 26c:	89 46 14             	mov    %eax,0x14(%rsi)
 26f:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 275:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 279:	c3                   	ret

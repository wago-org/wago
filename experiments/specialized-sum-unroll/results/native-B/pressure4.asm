
experiments/specialized-sum-unroll/results/native-B/pressure4.bin:     file format binary


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
  1e:	4c 8b 57 28          	mov    0x28(%rdi),%r10
  22:	4c 8b 5f 30          	mov    0x30(%rdi),%r11
  26:	e8 11 00 00 00       	call   0x3c
  2b:	5f                   	pop    %rdi
  2c:	48 89 07             	mov    %rax,(%rdi)
  2f:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  33:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  37:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  3b:	c3                   	ret
  3c:	48 81 ec 58 00 00 00 	sub    $0x58,%rsp
  43:	41 89 c4             	mov    %eax,%r12d
  46:	41 89 cd             	mov    %ecx,%r13d
  49:	49 89 d6             	mov    %rdx,%r14
  4c:	4c 89 c5             	mov    %r8,%rbp
  4f:	4c 89 4c 24 18       	mov    %r9,0x18(%rsp)
  54:	4c 89 54 24 20       	mov    %r10,0x20(%rsp)
  59:	4c 89 5c 24 28       	mov    %r11,0x28(%rsp)
  5e:	66 90                	xchg   %ax,%ax
  60:	45 85 ed             	test   %r13d,%r13d
  63:	0f 84 ea 00 00 00    	je     0x153
  69:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  6e:	44 89 ef             	mov    %r13d,%edi
  71:	48 c1 e7 03          	shl    $0x3,%rdi
  75:	45 89 e4             	mov    %r12d,%r12d
  78:	4c 01 e7             	add    %r12,%rdi
  7b:	4c 39 ff             	cmp    %r15,%rdi
  7e:	0f 86 20 00 00 00    	jbe    0xa4
  84:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  8b:	00 00 00 
  8e:	4c 39 ff             	cmp    %r15,%rdi
  91:	0f 85 ff 00 00 00    	jne    0x196
  97:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  9e:	0f 85 f2 00 00 00    	jne    0x196
  a4:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  a8:	41 83 c4 08          	add    $0x8,%r12d
  ac:	31 ff                	xor    %edi,%edi
  ae:	31 f6                	xor    %esi,%esi
  b0:	45 31 c9             	xor    %r9d,%r9d
  b3:	45 31 d2             	xor    %r10d,%r10d
  b6:	45 31 db             	xor    %r11d,%r11d
  b9:	31 c0                	xor    %eax,%eax
  bb:	31 d2                	xor    %edx,%edx
  bd:	41 83 ed 01          	sub    $0x1,%r13d
  c1:	0f 84 77 00 00 00    	je     0x13e
  c7:	41 83 fd 08          	cmp    $0x8,%r13d
  cb:	0f 82 52 00 00 00    	jb     0x123
  d1:	44 89 ef             	mov    %r13d,%edi
  d4:	48 c1 e7 03          	shl    $0x3,%rdi
  d8:	4c 01 e7             	add    %r12,%rdi
  db:	48 c1 ef 20          	shr    $0x20,%rdi
  df:	bf 00 00 00 00       	mov    $0x0,%edi
  e4:	0f 85 39 00 00 00    	jne    0x123
  ea:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  ee:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
  f3:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
  f8:	4e 03 4c 23 18       	add    0x18(%rbx,%r12,1),%r9
  fd:	4e 03 54 23 20       	add    0x20(%rbx,%r12,1),%r10
 102:	4e 03 5c 23 28       	add    0x28(%rbx,%r12,1),%r11
 107:	4a 03 44 23 30       	add    0x30(%rbx,%r12,1),%rax
 10c:	4a 03 54 23 38       	add    0x38(%rbx,%r12,1),%rdx
 111:	41 83 c4 40          	add    $0x40,%r12d
 115:	41 83 ed 08          	sub    $0x8,%r13d
 119:	41 83 fd 08          	cmp    $0x8,%r13d
 11d:	0f 83 c7 ff ff ff    	jae    0xea
 123:	45 85 ed             	test   %r13d,%r13d
 126:	0f 84 12 00 00 00    	je     0x13e
 12c:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 130:	41 83 c4 08          	add    $0x8,%r12d
 134:	41 83 ed 01          	sub    $0x1,%r13d
 138:	0f 85 ee ff ff ff    	jne    0x12c
 13e:	49 01 fe             	add    %rdi,%r14
 141:	49 01 f6             	add    %rsi,%r14
 144:	4d 01 ce             	add    %r9,%r14
 147:	4d 01 d6             	add    %r10,%r14
 14a:	4d 01 de             	add    %r11,%r14
 14d:	49 01 c6             	add    %rax,%r14
 150:	49 01 d6             	add    %rdx,%r14
 153:	4c 89 74 24 30       	mov    %r14,0x30(%rsp)
 158:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 15d:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
 162:	48 8d 7d 00          	lea    0x0(%rbp),%rdi
 166:	48 03 7c 24 18       	add    0x18(%rsp),%rdi
 16b:	48 03 7c 24 20       	add    0x20(%rsp),%rdi
 170:	48 03 7c 24 28       	add    0x28(%rsp),%rdi
 175:	48 89 7c 24 48       	mov    %rdi,0x48(%rsp)
 17a:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
 17f:	48 8b 54 24 38       	mov    0x38(%rsp),%rdx
 184:	48 8b 4c 24 40       	mov    0x40(%rsp),%rcx
 189:	4c 8b 44 24 48       	mov    0x48(%rsp),%r8
 18e:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
 195:	c3                   	ret
 196:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 19b:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 19f:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1a6:	89 46 14             	mov    %eax,0x14(%rsi)
 1a9:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1af:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1b3:	c3                   	ret

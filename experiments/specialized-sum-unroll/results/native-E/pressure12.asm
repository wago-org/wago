
experiments/specialized-sum-unroll/results/native-E/pressure12.bin:     file format binary


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
  8b:	0f 84 bf 00 00 00    	je     0x150
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
  b9:	0f 85 25 01 00 00    	jne    0x1e4
  bf:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  c6:	0f 85 18 01 00 00    	jne    0x1e4
  cc:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  d0:	41 83 c4 08          	add    $0x8,%r12d
  d4:	31 ff                	xor    %edi,%edi
  d6:	31 f6                	xor    %esi,%esi
  d8:	31 c0                	xor    %eax,%eax
  da:	41 83 ed 01          	sub    $0x1,%r13d
  de:	0f 84 63 00 00 00    	je     0x147
  e4:	41 83 fd 04          	cmp    $0x4,%r13d
  e8:	0f 82 3e 00 00 00    	jb     0x12c
  ee:	44 89 ef             	mov    %r13d,%edi
  f1:	48 c1 e7 03          	shl    $0x3,%rdi
  f5:	4c 01 e7             	add    %r12,%rdi
  f8:	48 c1 ef 20          	shr    $0x20,%rdi
  fc:	bf 00 00 00 00       	mov    $0x0,%edi
 101:	0f 85 25 00 00 00    	jne    0x12c
 107:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 10b:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
 110:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
 115:	4a 03 44 23 18       	add    0x18(%rbx,%r12,1),%rax
 11a:	41 83 c4 20          	add    $0x20,%r12d
 11e:	41 83 ed 04          	sub    $0x4,%r13d
 122:	41 83 fd 04          	cmp    $0x4,%r13d
 126:	0f 83 db ff ff ff    	jae    0x107
 12c:	45 85 ed             	test   %r13d,%r13d
 12f:	0f 84 12 00 00 00    	je     0x147
 135:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 139:	41 83 c4 08          	add    $0x8,%r12d
 13d:	41 83 ed 01          	sub    $0x1,%r13d
 141:	0f 85 ee ff ff ff    	jne    0x135
 147:	49 01 fe             	add    %rdi,%r14
 14a:	49 01 f6             	add    %rsi,%r14
 14d:	49 01 c6             	add    %rax,%r14
 150:	49 8d 39             	lea    (%r9),%rdi
 153:	49 8d 34 3a          	lea    (%r10,%rdi,1),%rsi
 157:	49 8d 3c 33          	lea    (%r11,%rsi,1),%rdi
 15b:	48 8d 74 3d 00       	lea    0x0(%rbp,%rdi,1),%rsi
 160:	48 03 74 24 40       	add    0x40(%rsp),%rsi
 165:	48 03 74 24 48       	add    0x48(%rsp),%rsi
 16a:	4c 89 b4 24 80 00 00 	mov    %r14,0x80(%rsp)
 171:	00 
 172:	4c 89 a4 24 88 00 00 	mov    %r12,0x88(%rsp)
 179:	00 
 17a:	4c 89 ac 24 90 00 00 	mov    %r13,0x90(%rsp)
 181:	00 
 182:	48 03 74 24 50       	add    0x50(%rsp),%rsi
 187:	48 03 74 24 58       	add    0x58(%rsp),%rsi
 18c:	48 03 74 24 60       	add    0x60(%rsp),%rsi
 191:	48 03 74 24 68       	add    0x68(%rsp),%rsi
 196:	48 03 74 24 70       	add    0x70(%rsp),%rsi
 19b:	48 03 74 24 78       	add    0x78(%rsp),%rsi
 1a0:	48 89 b4 24 98 00 00 	mov    %rsi,0x98(%rsp)
 1a7:	00 
 1a8:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 1ad:	48 8b 84 24 80 00 00 	mov    0x80(%rsp),%rax
 1b4:	00 
 1b5:	48 89 07             	mov    %rax,(%rdi)
 1b8:	48 8b 84 24 88 00 00 	mov    0x88(%rsp),%rax
 1bf:	00 
 1c0:	48 89 47 08          	mov    %rax,0x8(%rdi)
 1c4:	48 8b 84 24 90 00 00 	mov    0x90(%rsp),%rax
 1cb:	00 
 1cc:	48 89 47 10          	mov    %rax,0x10(%rdi)
 1d0:	48 8b 84 24 98 00 00 	mov    0x98(%rsp),%rax
 1d7:	00 
 1d8:	48 89 47 18          	mov    %rax,0x18(%rdi)
 1dc:	48 81 c4 a8 00 00 00 	add    $0xa8,%rsp
 1e3:	c3                   	ret
 1e4:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 1e9:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 1ed:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1f4:	89 46 14             	mov    %eax,0x14(%rsi)
 1f7:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1fd:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 201:	c3                   	ret

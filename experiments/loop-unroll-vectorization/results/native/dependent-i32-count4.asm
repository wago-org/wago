
experiments/loop-unroll-vectorization/results/native/dependent-i32-count4.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	4c 8b 47 18          	mov    0x18(%rdi),%r8
  1a:	e8 11 00 00 00       	call   0x30
  1f:	5f                   	pop    %rdi
  20:	48 89 07             	mov    %rax,(%rdi)
  23:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  27:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2b:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  2f:	c3                   	ret
  30:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  37:	41 89 c4             	mov    %eax,%r12d
  3a:	41 89 cd             	mov    %ecx,%r13d
  3d:	41 89 d6             	mov    %edx,%r14d
  40:	45 89 c1             	mov    %r8d,%r9d
  43:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  4a:	00 00 
  4c:	0f 1f 40 00          	nopl   0x0(%rax)
  50:	41 83 fe 04          	cmp    $0x4,%r14d
  54:	0f 82 cc 00 00 00    	jb     0x126
  5a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  5f:	45 89 ed             	mov    %r13d,%r13d
  62:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  66:	4c 39 ff             	cmp    %r15,%rdi
  69:	0f 87 34 01 00 00    	ja     0x1a3
  6f:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
  73:	45 89 e4             	mov    %r12d,%r12d
  76:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  7b:	4c 39 ff             	cmp    %r15,%rdi
  7e:	0f 87 1f 01 00 00    	ja     0x1a3
  84:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
  88:	41 83 c4 04          	add    $0x4,%r12d
  8c:	41 83 c5 04          	add    $0x4,%r13d
  90:	41 83 ee 01          	sub    $0x1,%r14d
  94:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  98:	4c 39 ff             	cmp    %r15,%rdi
  9b:	0f 87 02 01 00 00    	ja     0x1a3
  a1:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
  a5:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  aa:	4c 39 ff             	cmp    %r15,%rdi
  ad:	0f 87 f0 00 00 00    	ja     0x1a3
  b3:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
  b7:	41 83 c4 04          	add    $0x4,%r12d
  bb:	41 83 c5 04          	add    $0x4,%r13d
  bf:	41 83 ee 01          	sub    $0x1,%r14d
  c3:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  c7:	4c 39 ff             	cmp    %r15,%rdi
  ca:	0f 87 d3 00 00 00    	ja     0x1a3
  d0:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
  d4:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  d9:	4c 39 ff             	cmp    %r15,%rdi
  dc:	0f 87 c1 00 00 00    	ja     0x1a3
  e2:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
  e6:	41 83 c4 04          	add    $0x4,%r12d
  ea:	41 83 c5 04          	add    $0x4,%r13d
  ee:	41 83 ee 01          	sub    $0x1,%r14d
  f2:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  f6:	4c 39 ff             	cmp    %r15,%rdi
  f9:	0f 87 a4 00 00 00    	ja     0x1a3
  ff:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
 103:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 108:	4c 39 ff             	cmp    %r15,%rdi
 10b:	0f 87 92 00 00 00    	ja     0x1a3
 111:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
 115:	41 83 c4 04          	add    $0x4,%r12d
 119:	41 83 c5 04          	add    $0x4,%r13d
 11d:	41 83 ee 01          	sub    $0x1,%r14d
 121:	e9 2a ff ff ff       	jmp    0x50
 126:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 12d:	00 00 
 12f:	90                   	nop
 130:	45 85 f6             	test   %r14d,%r14d
 133:	0f 84 3a 00 00 00    	je     0x173
 139:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 13e:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 142:	4c 39 ff             	cmp    %r15,%rdi
 145:	0f 87 58 00 00 00    	ja     0x1a3
 14b:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
 14f:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 154:	4c 39 ff             	cmp    %r15,%rdi
 157:	0f 87 46 00 00 00    	ja     0x1a3
 15d:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
 161:	41 83 c4 04          	add    $0x4,%r12d
 165:	41 83 c5 04          	add    $0x4,%r13d
 169:	41 83 ee 01          	sub    $0x1,%r14d
 16d:	0f 85 cb ff ff ff    	jne    0x13e
 173:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
 178:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
 17d:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
 182:	4c 89 4c 24 28       	mov    %r9,0x28(%rsp)
 187:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
 18c:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
 191:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
 196:	4c 8b 44 24 28       	mov    0x28(%rsp),%r8
 19b:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 1a2:	c3                   	ret
 1a3:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 1a8:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 1ac:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1b3:	89 46 14             	mov    %eax,0x14(%rsi)
 1b6:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1bc:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1c0:	c3                   	ret

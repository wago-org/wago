module top(input clk, input rst, input en, output reg [7:0] count);
  always @(posedge clk) begin
    if (rst) count <= 8'h00;
    else if (en) count <= count + 8'h01;
  end
endmodule

int dot_product(const int *left, const int *right, int length) {
    int sum = 0;
    for (int i = 0; i < length; ++i) {
        sum += left[i] * right[i];
    }
    return sum;
}

int count_below(const int *values, int length, int threshold) {
    int count = 0;
    for (int i = 0; i < length; ++i) {
        if (values[i] < threshold) {
            ++count;
        }
    }
    return count;
}

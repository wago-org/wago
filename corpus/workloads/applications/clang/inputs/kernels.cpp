template <typename T>
T weighted_sum(const T *values, int length, T weight) {
    T sum = 0;
    for (int i = 0; i < length; ++i) {
        sum += values[i] * weight;
    }
    return sum;
}

extern "C" int run_kernel(const int *values, int length) {
    return weighted_sum(values, length, 7);
}
